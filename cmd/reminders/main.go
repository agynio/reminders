package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openziti/sdk-golang/ziti"

	"github.com/agynio/reminders/internal/api"
	"github.com/agynio/reminders/internal/config"
	"github.com/agynio/reminders/internal/db"
	"github.com/agynio/reminders/internal/enrollment"
	"github.com/agynio/reminders/internal/gateway"
	"github.com/agynio/reminders/internal/scheduler"
	"github.com/agynio/reminders/internal/store"
)

const (
	shutdownTimeout = 10 * time.Second
	retryBackoff    = 30 * time.Second
	maxFireAttempts = 3
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("reminders: %v", err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("create connection pool: %w", err)
	}
	defer pool.Close()

	if err := db.ApplyMigrations(ctx, pool); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	enrolled, err := enrollment.Enroll(ctx, cfg.GatewayURL, cfg.ServiceToken)
	if err != nil {
		return fmt.Errorf("enroll app: %w", err)
	}
	if err := os.WriteFile(cfg.ZitiIdentityFile, enrolled.IdentityJSON, 0600); err != nil {
		return fmt.Errorf("write ziti identity: %w", err)
	}

	zitiCfg, err := ziti.NewConfigFromFile(cfg.ZitiIdentityFile)
	if err != nil {
		return fmt.Errorf("load ziti identity: %w", err)
	}
	zitiCtx, err := ziti.NewContext(zitiCfg)
	if err != nil {
		return fmt.Errorf("create ziti context: %w", err)
	}

	reminderStore := store.NewStore(pool)
	gatewayClient := gateway.NewClient(zitiCtx, cfg.GatewayServiceName, enrolled.IdentityID)
	retryTracker := newRetryTracker()
	var reminderScheduler *scheduler.Scheduler
	reminderScheduler = scheduler.New(func(ctx context.Context, reminderID uuid.UUID) {
		_ = fireReminder(ctx, reminderStore, gatewayClient, reminderScheduler, retryTracker, reminderID)
	})
	defer reminderScheduler.Stop()

	pending, err := reminderStore.LoadPending(ctx)
	if err != nil {
		return fmt.Errorf("load pending reminders: %w", err)
	}
	for _, reminder := range pending {
		reminderScheduler.Schedule(reminder.ID, reminder.At)
	}
	log.Printf("Loaded %d pending reminders", len(pending))

	handler := api.NewHandler(reminderStore, reminderScheduler)
	mux := http.NewServeMux()
	mux.Handle("/create-reminder", api.RequireIdentity(http.HandlerFunc(handler.CreateReminder)))
	mux.Handle("/cancel-reminder", api.RequireIdentity(http.HandlerFunc(handler.CancelReminder)))
	mux.Handle("/list-reminders", api.RequireIdentity(http.HandlerFunc(handler.ListReminders)))
	mux.Handle("/get-reminder", api.RequireIdentity(http.HandlerFunc(handler.GetReminder)))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	zitiListener, err := zitiCtx.ListenWithOptions(cfg.ZitiServiceName, &ziti.ListenOptions{})
	if err != nil {
		return fmt.Errorf("listen on ziti service %s: %w", cfg.ZitiServiceName, err)
	}
	tcpListener, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddress, err)
	}

	zitiServer := &http.Server{Handler: mux}
	tcpServer := &http.Server{Handler: mux}
	errCh := make(chan error, 2)

	go func() {
		if err := zitiServer.Serve(zitiListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve ziti listener: %w", err)
		}
	}()
	go func() {
		if err := tcpServer.Serve(tcpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve tcp listener: %w", err)
		}
	}()

	log.Printf("Reminders listening on %s (ziti service)", cfg.ZitiServiceName)
	log.Printf("Health endpoints listening on %s", cfg.HTTPAddress)

	var serveErr error
	select {
	case <-ctx.Done():
	case serveErr = <-errCh:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	zitiErr := zitiServer.Shutdown(shutdownCtx)
	if zitiErr != nil {
		zitiErr = fmt.Errorf("shutdown ziti server: %w", zitiErr)
	}
	tcpErr := tcpServer.Shutdown(shutdownCtx)
	if tcpErr != nil {
		tcpErr = fmt.Errorf("shutdown tcp server: %w", tcpErr)
	}
	shutdownErr := errors.Join(zitiErr, tcpErr)
	if shutdownErr != nil {
		return shutdownErr
	}

	return serveErr
}

func fireReminder(
	ctx context.Context,
	s *store.Store,
	client *gateway.Client,
	scheduler *scheduler.Scheduler,
	retries *retryTracker,
	reminderID uuid.UUID,
) error {
	attempt := retries.Next(reminderID)
	reminder, err := s.GetReminder(ctx, reminderID)
	if err != nil {
		retries.Reset(reminderID)
		log.Printf("fire reminder_id=%s thread_id=unknown attempt=%d error=%v", reminderID, attempt, err)
		return err
	}
	threadID := reminder.ThreadID
	if reminder.Status != store.ReminderStatusPending {
		retries.Reset(reminderID)
		log.Printf(
			"fire reminder_id=%s thread_id=%s attempt=%d status=%s action=skip",
			reminderID,
			threadID,
			attempt,
			reminder.Status,
		)
		return nil
	}
	if err := client.SendMessage(ctx, threadID, reminder.Note); err != nil {
		if attempt < maxFireAttempts {
			scheduler.Schedule(reminderID, time.Now().Add(retryBackoff))
			log.Printf(
				"fire reminder_id=%s thread_id=%s attempt=%d action=reschedule delay=%s error=%v",
				reminderID,
				threadID,
				attempt,
				retryBackoff,
				err,
			)
			return err
		}
		retries.Reset(reminderID)
		log.Printf(
			"fire reminder_id=%s thread_id=%s attempt=%d action=give_up error=%v",
			reminderID,
			threadID,
			attempt,
			err,
		)
		return err
	}
	if _, err := s.CompleteReminder(ctx, reminderID); err != nil {
		retries.Reset(reminderID)
		log.Printf(
			"fire reminder_id=%s thread_id=%s attempt=%d action=complete_failed error=%v",
			reminderID,
			threadID,
			attempt,
			err,
		)
		return err
	}
	retries.Reset(reminderID)
	log.Printf(
		"fire reminder_id=%s thread_id=%s attempt=%d action=complete",
		reminderID,
		threadID,
		attempt,
	)
	return nil
}

type retryTracker struct {
	mu       sync.Mutex
	attempts map[uuid.UUID]int
}

func newRetryTracker() *retryTracker {
	return &retryTracker{attempts: make(map[uuid.UUID]int)}
}

func (r *retryTracker) Next(id uuid.UUID) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attempts[id]++
	return r.attempts[id]
}

func (r *retryTracker) Reset(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.attempts, id)
}

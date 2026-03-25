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
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openziti/sdk-golang/ziti"

	"github.com/agynio/reminders/internal/api"
	"github.com/agynio/reminders/internal/config"
	"github.com/agynio/reminders/internal/db"
	"github.com/agynio/reminders/internal/gateway"
	"github.com/agynio/reminders/internal/scheduler"
	"github.com/agynio/reminders/internal/store"
)

const shutdownTimeout = 10 * time.Second

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

	zitiCfg, err := ziti.NewConfigFromFile(cfg.ZitiIdentityFile)
	if err != nil {
		return fmt.Errorf("load ziti identity: %w", err)
	}
	zitiCtx, err := ziti.NewContext(zitiCfg)
	if err != nil {
		return fmt.Errorf("create ziti context: %w", err)
	}

	reminderStore := store.NewStore(pool)
	gatewayClient := gateway.NewClient(zitiCtx, cfg.GatewayServiceName, cfg.AppIdentityID)
	scheduler := scheduler.New(func(ctx context.Context, reminderID uuid.UUID) {
		fireReminder(ctx, reminderStore, gatewayClient, reminderID)
	})
	defer scheduler.Stop()

	pending, err := reminderStore.LoadPending(ctx)
	if err != nil {
		return fmt.Errorf("load pending reminders: %w", err)
	}
	for _, reminder := range pending {
		scheduler.Schedule(reminder.ID, reminder.At)
	}
	log.Printf("Loaded %d pending reminders", len(pending))

	handler := api.NewHandler(reminderStore, scheduler)
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

	if err := zitiServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown ziti server: %w", err)
	}
	if err := tcpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown tcp server: %w", err)
	}

	return serveErr
}

func fireReminder(ctx context.Context, s *store.Store, client *gateway.Client, reminderID uuid.UUID) {
	reminder, err := s.GetReminder(ctx, reminderID)
	if err != nil {
		log.Printf("fire %s: load: %v", reminderID, err)
		return
	}
	if reminder.Status != store.ReminderStatusPending {
		log.Printf("fire %s: status is %q, skipping", reminderID, reminder.Status)
		return
	}
	if err := client.SendMessage(ctx, reminder.ThreadID, reminder.Note); err != nil {
		log.Printf("fire %s: send message: %v", reminderID, err)
		return
	}
	if _, err := s.CompleteReminder(ctx, reminderID); err != nil {
		log.Printf("fire %s: complete: %v", reminderID, err)
		return
	}
	log.Printf("fired %s for thread %s", reminderID, reminder.ThreadID)
}

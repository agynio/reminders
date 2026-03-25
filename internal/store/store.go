package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type ReminderStatus string

const (
	ReminderStatusPending   ReminderStatus = "pending"
	ReminderStatusCompleted ReminderStatus = "completed"
	ReminderStatusCancelled ReminderStatus = "cancelled"
)

func ParseReminderStatus(value string) (ReminderStatus, error) {
	switch ReminderStatus(value) {
	case ReminderStatusPending, ReminderStatusCompleted, ReminderStatusCancelled:
		return ReminderStatus(value), nil
	default:
		return "", fmt.Errorf("invalid reminder status: %s", value)
	}
}

type Reminder struct {
	ID          uuid.UUID
	ThreadID    uuid.UUID
	IdentityID  uuid.UUID
	Note        string
	Status      ReminderStatus
	At          time.Time
	CreatedAt   time.Time
	CompletedAt *time.Time
	CancelledAt *time.Time
}

type CreateReminderInput struct {
	ThreadID     uuid.UUID
	IdentityID   uuid.UUID
	Note         string
	DelaySeconds int64
}

type rowScanner interface {
	Scan(...any) error
}

func scanReminder(scanner rowScanner) (Reminder, error) {
	var reminder Reminder
	var statusRaw string
	var completedAt sql.NullTime
	var cancelledAt sql.NullTime
	if err := scanner.Scan(
		&reminder.ID,
		&reminder.ThreadID,
		&reminder.IdentityID,
		&reminder.Note,
		&statusRaw,
		&reminder.At,
		&reminder.CreatedAt,
		&completedAt,
		&cancelledAt,
	); err != nil {
		return Reminder{}, err
	}
	status, err := ParseReminderStatus(statusRaw)
	if err != nil {
		return Reminder{}, err
	}
	reminder.Status = status
	reminder.CompletedAt = nullTimePtr(completedAt)
	reminder.CancelledAt = nullTimePtr(cancelledAt)
	return reminder, nil
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time
	return &timestamp
}

func (s *Store) runTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) CreateReminder(ctx context.Context, input CreateReminderInput) (Reminder, error) {
	row := s.pool.QueryRow(
		ctx,
		`INSERT INTO reminders (thread_id, identity_id, note, at)
         VALUES ($1, $2, $3, NOW() + $4 * interval '1 second')
         RETURNING id, thread_id, identity_id, note, status, at, created_at, completed_at, cancelled_at`,
		input.ThreadID,
		input.IdentityID,
		input.Note,
		input.DelaySeconds,
	)
	reminder, err := scanReminder(row)
	if err != nil {
		return Reminder{}, err
	}
	return reminder, nil
}

func (s *Store) GetReminder(ctx context.Context, id uuid.UUID) (Reminder, error) {
	row := s.pool.QueryRow(
		ctx,
		`SELECT id, thread_id, identity_id, note, status, at, created_at, completed_at, cancelled_at
         FROM reminders WHERE id = $1`,
		id,
	)
	reminder, err := scanReminder(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Reminder{}, NotFoundError{ReminderID: id}
		}
		return Reminder{}, err
	}
	return reminder, nil
}

func (s *Store) CancelReminder(ctx context.Context, id uuid.UUID) (Reminder, error) {
	return s.updateStatus(ctx, id, ReminderStatusCancelled)
}

func (s *Store) CompleteReminder(ctx context.Context, id uuid.UUID) (Reminder, error) {
	return s.updateStatus(ctx, id, ReminderStatusCompleted)
}

func (s *Store) updateStatus(ctx context.Context, id uuid.UUID, status ReminderStatus) (Reminder, error) {
	var reminder Reminder
	err := s.runTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(
			ctx,
			`UPDATE reminders
             SET status = $1,
                 completed_at = CASE WHEN $1 = 'completed' THEN NOW() ELSE completed_at END,
                 cancelled_at = CASE WHEN $1 = 'cancelled' THEN NOW() ELSE cancelled_at END
             WHERE id = $2 AND status = 'pending'
             RETURNING id, thread_id, identity_id, note, status, at, created_at, completed_at, cancelled_at`,
			status,
			id,
		)
		updated, err := scanReminder(row)
		if err == nil {
			reminder = updated
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var currentStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM reminders WHERE id = $1`, id).Scan(&currentStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return NotFoundError{ReminderID: id}
			}
			return err
		}
		parsedStatus, err := ParseReminderStatus(currentStatus)
		if err != nil {
			return err
		}
		return InvalidStatusError{ReminderID: id, Status: parsedStatus}
	})
	if err != nil {
		return Reminder{}, err
	}
	return reminder, nil
}

func (s *Store) ListReminders(ctx context.Context, threadID uuid.UUID, status *ReminderStatus) ([]Reminder, error) {
	if status == nil {
		rows, err := s.pool.Query(
			ctx,
			`SELECT id, thread_id, identity_id, note, status, at, created_at, completed_at, cancelled_at
             FROM reminders
             WHERE thread_id = $1
             ORDER BY at ASC`,
			threadID,
		)
		if err != nil {
			return nil, err
		}
		return scanReminders(rows)
	}

	rows, err := s.pool.Query(
		ctx,
		`SELECT id, thread_id, identity_id, note, status, at, created_at, completed_at, cancelled_at
         FROM reminders
         WHERE thread_id = $1 AND status = $2
         ORDER BY at ASC`,
		threadID,
		*status,
	)
	if err != nil {
		return nil, err
	}
	return scanReminders(rows)
}

func (s *Store) LoadPending(ctx context.Context) ([]Reminder, error) {
	rows, err := s.pool.Query(
		ctx,
		`SELECT id, thread_id, identity_id, note, status, at, created_at, completed_at, cancelled_at
         FROM reminders
         WHERE status = 'pending'
         ORDER BY at ASC`,
	)
	if err != nil {
		return nil, err
	}
	return scanReminders(rows)
}

func scanReminders(rows pgx.Rows) ([]Reminder, error) {
	defer rows.Close()
	reminders := []Reminder{}
	for rows.Next() {
		reminder, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		reminders = append(reminders, reminder)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reminders, nil
}

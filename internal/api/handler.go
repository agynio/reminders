package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/agynio/reminders/internal/scheduler"
	"github.com/agynio/reminders/internal/store"
)

const (
	maxDelaySeconds int64 = 604800
	statusAll             = "all"
)

type Handler struct {
	store     *store.Store
	scheduler *scheduler.Scheduler
}

func NewHandler(store *store.Store, scheduler *scheduler.Scheduler) *Handler {
	return &Handler{store: store, scheduler: scheduler}
}

func (h *Handler) CreateReminder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	identity, ok := IdentityFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing identity")
		return
	}

	var req createReminderRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	threadIDValue := firstNonEmpty(req.ThreadID, req.Thread)
	threadID, err := uuid.Parse(threadIDValue)
	if err != nil {
		writeError(w, http.StatusBadRequest, "thread_id must be a valid uuid")
		return
	}
	delaySeconds := firstInt64(req.DelaySeconds, req.Delay)
	if delaySeconds == nil {
		writeError(w, http.StatusBadRequest, "delay_seconds must be provided")
		return
	}
	if *delaySeconds < 0 || *delaySeconds > maxDelaySeconds {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("delay_seconds must be between 0 and %d", maxDelaySeconds))
		return
	}
	note := strings.TrimSpace(req.Note)
	if note == "" {
		writeError(w, http.StatusBadRequest, "note must be provided")
		return
	}

	reminder, err := h.store.CreateReminder(r.Context(), store.CreateReminderInput{
		ThreadID:     threadID,
		IdentityID:   identity.ID,
		Note:         note,
		DelaySeconds: *delaySeconds,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create reminder")
		return
	}

	h.scheduler.Schedule(reminder.ID, reminder.At)
	writeJSON(w, http.StatusCreated, singleReminderResponse{Reminder: reminderToResponse(reminder)})
}

func (h *Handler) CancelReminder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req cancelReminderRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	reminderID, err := uuid.Parse(firstNonEmpty(req.ReminderID, req.ID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "reminder_id must be a valid uuid")
		return
	}

	reminder, err := h.store.CancelReminder(r.Context(), reminderID)
	if err != nil {
		var notFound store.NotFoundError
		var invalidStatus store.InvalidStatusError
		switch {
		case errors.As(err, &notFound):
			writeError(w, http.StatusNotFound, "reminder not found")
			return
		case errors.As(err, &invalidStatus):
			writeError(w, http.StatusConflict, "reminder is not pending")
			return
		default:
			writeError(w, http.StatusInternalServerError, "failed to cancel reminder")
			return
		}
	}

	h.scheduler.Cancel(reminder.ID)
	writeJSON(w, http.StatusOK, singleReminderResponse{Reminder: reminderToResponse(reminder)})
}

func (h *Handler) ListReminders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req listRemindersRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	threadID, err := uuid.Parse(firstNonEmpty(req.ThreadID, req.Thread))
	if err != nil {
		writeError(w, http.StatusBadRequest, "thread_id must be a valid uuid")
		return
	}

	statusFilter, err := parseStatusFilter(req.Status)
	if err != nil {
		writeError(
			w,
			http.StatusBadRequest,
			fmt.Sprintf(
				"status must be %s, %s, %s, or %s",
				store.ReminderStatusPending,
				store.ReminderStatusCompleted,
				store.ReminderStatusCancelled,
				statusAll,
			),
		)
		return
	}

	reminders, err := h.store.ListReminders(r.Context(), threadID, statusFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list reminders")
		return
	}

	responses := make([]reminderResponse, len(reminders))
	for i, reminder := range reminders {
		responses[i] = reminderToResponse(reminder)
	}

	writeJSON(w, http.StatusOK, listRemindersResponse{Reminders: responses})
}

func (h *Handler) GetReminder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req getReminderRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	reminderID, err := uuid.Parse(firstNonEmpty(req.ReminderID, req.ID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "reminder_id must be a valid uuid")
		return
	}

	reminder, err := h.store.GetReminder(r.Context(), reminderID)
	if err != nil {
		var notFound store.NotFoundError
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, "reminder not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get reminder")
		return
	}

	writeJSON(w, http.StatusOK, singleReminderResponse{Reminder: reminderToResponse(reminder)})
}

type createReminderRequest struct {
	ThreadID     string `json:"thread_id"`
	Thread       string `json:"thread"`
	DelaySeconds *int64 `json:"delay_seconds"`
	Delay        *int64 `json:"delay"`
	Note         string `json:"note"`
}

type cancelReminderRequest struct {
	ReminderID string `json:"reminder_id"`
	ID         string `json:"id"`
}

type listRemindersRequest struct {
	ThreadID string `json:"thread_id"`
	Thread   string `json:"thread"`
	Status   string `json:"status"`
}

type getReminderRequest struct {
	ReminderID string `json:"reminder_id"`
	ID         string `json:"id"`
}

type reminderResponse struct {
	ID          string     `json:"id"`
	ThreadID    string     `json:"thread_id"`
	IdentityID  string     `json:"identity_id"`
	Note        string     `json:"note"`
	Status      string     `json:"status"`
	At          time.Time  `json:"at"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
	CancelledAt *time.Time `json:"cancelled_at"`
}

type singleReminderResponse struct {
	Reminder reminderResponse `json:"reminder"`
}

type listRemindersResponse struct {
	Reminders []reminderResponse `json:"reminders"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func reminderToResponse(reminder store.Reminder) reminderResponse {
	return reminderResponse{
		ID:          reminder.ID.String(),
		ThreadID:    reminder.ThreadID.String(),
		IdentityID:  reminder.IdentityID.String(),
		Note:        reminder.Note,
		Status:      string(reminder.Status),
		At:          reminder.At,
		CreatedAt:   reminder.CreatedAt,
		CompletedAt: reminder.CompletedAt,
		CancelledAt: reminder.CancelledAt,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstInt64(values ...*int64) *int64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid json: %v", err))
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		log.Printf("writeJSON: write: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func parseStatusFilter(raw string) (*store.ReminderStatus, error) {
	status := strings.ToLower(strings.TrimSpace(raw))
	if status == "" {
		pending := store.ReminderStatusPending
		return &pending, nil
	}
	if status == statusAll {
		return nil, nil
	}
	parsed, err := store.ParseReminderStatus(status)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/agynio/reminders/internal/scheduler"
	"github.com/agynio/reminders/internal/store"
)

const maxDelaySeconds int64 = 604800

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

	threadID, err := uuid.Parse(req.ThreadID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "thread_id must be a valid uuid")
		return
	}
	if req.DelaySeconds == nil {
		writeError(w, http.StatusBadRequest, "delay_seconds must be provided")
		return
	}
	if *req.DelaySeconds < 0 || *req.DelaySeconds > maxDelaySeconds {
		writeError(w, http.StatusBadRequest, "delay_seconds must be between 0 and 604800")
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
		DelaySeconds: *req.DelaySeconds,
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

	reminderID, err := uuid.Parse(req.ReminderID)
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

	threadID, err := uuid.Parse(req.ThreadID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "thread_id must be a valid uuid")
		return
	}

	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status == "" {
		status = string(store.ReminderStatusPending)
	}
	if !isValidStatus(status) {
		writeError(w, http.StatusBadRequest, "status must be pending, completed, cancelled, or all")
		return
	}

	reminders, err := h.store.ListReminders(r.Context(), threadID, status)
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

	reminderID, err := uuid.Parse(req.ReminderID)
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
	DelaySeconds *int64 `json:"delay_seconds"`
	Note         string `json:"note"`
}

type cancelReminderRequest struct {
	ReminderID string `json:"reminder_id"`
}

type listRemindersRequest struct {
	ThreadID string `json:"thread_id"`
	Status   string `json:"status"`
}

type getReminderRequest struct {
	ReminderID string `json:"reminder_id"`
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

func isValidStatus(status string) bool {
	switch status {
	case string(store.ReminderStatusPending), string(store.ReminderStatusCompleted), string(store.ReminderStatusCancelled), "all":
		return true
	default:
		return false
	}
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

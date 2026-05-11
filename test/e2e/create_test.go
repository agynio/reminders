//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCreateReminder(t *testing.T) {
	t.Run("pascal case proxy path", func(t *testing.T) {
		threadID := randomThreadID()
		resp := postJSON(t, "/CreateReminder", map[string]any{
			"thread_id":     threadID,
			"delay_seconds": int64(3600),
			"note":          "pascal case " + uuid.NewString(),
		})
		require.Equal(t, http.StatusCreated, resp.StatusCode)
		body := decodeResponse[singleReminderResponse](t, resp)
		require.Equal(t, threadID, body.Reminder.ThreadID)
	})

	threadID := randomThreadID()
	note := "  reminder " + uuid.NewString() + "  "

	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     threadID,
		"delay_seconds": int64(3600),
		"note":          note,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	body := decodeResponse[singleReminderResponse](t, resp)
	reminder := body.Reminder

	require.NotEmpty(t, reminder.ID)
	require.Equal(t, threadID, reminder.ThreadID)
	require.Equal(t, testIdentityID, reminder.IdentityID)
	require.Equal(t, strings.TrimSpace(note), reminder.Note)
	require.Equal(t, "pending", reminder.Status)
	require.NotEmpty(t, reminder.At)
	require.NotEmpty(t, reminder.CreatedAt)
	require.Nil(t, reminder.CompletedAt)
	require.Nil(t, reminder.CancelledAt)
	_ = parseTimestamp(t, reminder.At)
	_ = parseTimestamp(t, reminder.CreatedAt)
}

func TestCreateReminder_ZeroDelay(t *testing.T) {
	threadID := randomThreadID()
	start := time.Now().UTC()

	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     threadID,
		"delay_seconds": int64(0),
		"note":          "zero delay " + uuid.NewString(),
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	body := decodeResponse[singleReminderResponse](t, resp)
	reminder := body.Reminder

	at := parseTimestamp(t, reminder.At)
	require.WithinDuration(t, start, at, 5*time.Second)
	require.Equal(t, "pending", reminder.Status)
}

func TestCreateReminder_MaxDelay(t *testing.T) {
	threadID := randomThreadID()
	start := time.Now().UTC()
	expected := start.Add(7 * 24 * time.Hour)

	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     threadID,
		"delay_seconds": int64(604800),
		"note":          "max delay " + uuid.NewString(),
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	body := decodeResponse[singleReminderResponse](t, resp)
	reminder := body.Reminder

	at := parseTimestamp(t, reminder.At)
	require.WithinDuration(t, expected, at, 5*time.Second)
	require.Equal(t, "pending", reminder.Status)
}

func TestCreateReminder_MissingIdentity(t *testing.T) {
	resp := postJSONNoIdentity(t, "/create-reminder", map[string]any{
		"thread_id":     randomThreadID(),
		"delay_seconds": int64(60),
		"note":          "missing identity",
	})
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "missing identity", body.Error)
}

func TestCreateReminder_InvalidThreadID(t *testing.T) {
	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     "not-a-uuid",
		"delay_seconds": int64(60),
		"note":          "invalid thread",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "thread_id must be a valid uuid", body.Error)
}

func TestCreateReminder_MissingDelaySeconds(t *testing.T) {
	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id": randomThreadID(),
		"note":      "missing delay",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "delay_seconds must be provided", body.Error)
}

func TestCreateReminder_NegativeDelay(t *testing.T) {
	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     randomThreadID(),
		"delay_seconds": int64(-1),
		"note":          "negative delay",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "delay_seconds must be between 0 and 604800", body.Error)
}

func TestCreateReminder_ExceedsMaxDelay(t *testing.T) {
	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     randomThreadID(),
		"delay_seconds": int64(604801),
		"note":          "too long",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "delay_seconds must be between 0 and 604800", body.Error)
}

func TestCreateReminder_EmptyNote(t *testing.T) {
	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     randomThreadID(),
		"delay_seconds": int64(60),
		"note":          "",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "note must be provided", body.Error)
}

func TestCreateReminder_WhitespaceOnlyNote(t *testing.T) {
	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     randomThreadID(),
		"delay_seconds": int64(60),
		"note":          "   ",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "note must be provided", body.Error)
}

func parseTimestamp(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	require.NoError(t, err)
	return parsed
}

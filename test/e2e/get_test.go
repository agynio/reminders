//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGetReminder(t *testing.T) {
	threadID := randomThreadID()
	created := createTestReminder(t, threadID, 3600, "get reminder "+uuid.NewString())

	resp := postJSON(t, "/get-reminder", map[string]string{"reminder_id": created.ID})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decodeResponse[singleReminderResponse](t, resp)

	require.Equal(t, created, body.Reminder)
}

func TestGetReminder_NotFound(t *testing.T) {
	resp := postJSON(t, "/get-reminder", map[string]string{"reminder_id": uuid.NewString()})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "reminder not found", body.Error)
}

func TestGetReminder_InvalidID(t *testing.T) {
	resp := postJSON(t, "/get-reminder", map[string]string{"reminder_id": "bad"})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "reminder_id must be a valid uuid", body.Error)
}

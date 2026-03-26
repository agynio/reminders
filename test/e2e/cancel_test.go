//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCancelReminder(t *testing.T) {
	threadID := randomThreadID()
	created := createTestReminder(t, threadID, 3600, "cancel reminder "+uuid.NewString())

	resp := postJSON(t, "/cancel-reminder", map[string]string{"reminder_id": created.ID})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decodeResponse[singleReminderResponse](t, resp)
	reminder := body.Reminder

	require.Equal(t, created.ID, reminder.ID)
	require.Equal(t, "cancelled", reminder.Status)
	require.NotNil(t, reminder.CancelledAt)
	require.Nil(t, reminder.CompletedAt)
}

func TestCancelReminder_AlreadyCancelled(t *testing.T) {
	threadID := randomThreadID()
	created := createTestReminder(t, threadID, 3600, "already cancelled "+uuid.NewString())

	resp := postJSON(t, "/cancel-reminder", map[string]string{"reminder_id": created.ID})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = decodeResponse[singleReminderResponse](t, resp)

	secondResp := postJSON(t, "/cancel-reminder", map[string]string{"reminder_id": created.ID})
	require.Equal(t, http.StatusConflict, secondResp.StatusCode)
	body := decodeResponse[errorBody](t, secondResp)
	require.Equal(t, "reminder is not pending", body.Error)
}

func TestCancelReminder_NotFound(t *testing.T) {
	resp := postJSON(t, "/cancel-reminder", map[string]string{"reminder_id": uuid.NewString()})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "reminder not found", body.Error)
}

func TestCancelReminder_InvalidID(t *testing.T) {
	resp := postJSON(t, "/cancel-reminder", map[string]string{"reminder_id": "bad"})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "reminder_id must be a valid uuid", body.Error)
}

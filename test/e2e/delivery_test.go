//go:build e2e

package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDelivery_HappyPath(t *testing.T) {
	threadID := createThreadWithAppParticipant(t)
	note := "e2e delivery test " + uuid.NewString()

	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     threadID,
		"delay_seconds": int64(0),
		"note":          note,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decodeResponse[singleReminderResponse](t, resp)

	completed := pollReminderStatus(t, created.Reminder.ID, "completed", 30*time.Second)
	require.NotNil(t, completed.CompletedAt)

	messages := getThreadMessages(t, threadID)
	require.Len(t, messages, 1)
	require.Equal(t, "Reminder: "+note, messages[0].GetBody())
	require.Equal(t, appIdentityID, messages[0].GetSenderId())
}

func TestDelivery_CompletedAtIsSet(t *testing.T) {
	threadID := createThreadWithAppParticipant(t)
	note := "completed at test " + uuid.NewString()

	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     threadID,
		"delay_seconds": int64(0),
		"note":          note,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decodeResponse[singleReminderResponse](t, resp)

	completed := pollReminderStatus(t, created.Reminder.ID, "completed", 30*time.Second)
	require.NotNil(t, completed.CompletedAt)
	completedAt := parseTimestamp(t, *completed.CompletedAt)
	require.WithinDuration(t, time.Now(), completedAt, 1*time.Minute)
}

func TestDelivery_FailsWithoutThread(t *testing.T) {
	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     uuid.NewString(),
		"delay_seconds": int64(0),
		"note":          "should not deliver",
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decodeResponse[singleReminderResponse](t, resp)

	time.Sleep(15 * time.Second)
	reminder := getReminder(t, created.Reminder.ID)
	require.Equal(t, "pending", reminder.Status)
	require.Nil(t, reminder.CompletedAt)
}

func TestDelivery_CancelBeforeFire(t *testing.T) {
	threadID := createThreadWithAppParticipant(t)

	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     threadID,
		"delay_seconds": int64(3600),
		"note":          "should be cancelled",
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decodeResponse[singleReminderResponse](t, resp)

	cancelResp := postJSON(t, "/cancel-reminder", map[string]string{
		"reminder_id": created.Reminder.ID,
	})
	require.Equal(t, http.StatusOK, cancelResp.StatusCode)
	_ = decodeResponse[singleReminderResponse](t, cancelResp)

	messages := getThreadMessages(t, threadID)
	require.Empty(t, messages)
}

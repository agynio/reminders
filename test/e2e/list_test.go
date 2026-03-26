//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestListReminders_DefaultPending(t *testing.T) {
	threadID := randomThreadID()
	first := createTestReminder(t, threadID, 3600, "list pending "+uuid.NewString())
	second := createTestReminder(t, threadID, 3600, "list pending "+uuid.NewString())

	resp := listReminders(t, threadID, "")
	require.Len(t, resp.Reminders, 2)
	require.ElementsMatch(t, []string{first.ID, second.ID}, reminderIDs(resp.Reminders))
}

func TestListReminders_FilterPending(t *testing.T) {
	threadID := randomThreadID()
	pending := createTestReminder(t, threadID, 3600, "pending "+uuid.NewString())
	cancelled := createTestReminder(t, threadID, 3600, "cancelled "+uuid.NewString())

	cancelResp := postJSON(t, "/cancel-reminder", map[string]string{"reminder_id": cancelled.ID})
	require.Equal(t, http.StatusOK, cancelResp.StatusCode)
	_ = decodeResponse[singleReminderResponse](t, cancelResp)

	resp := listReminders(t, threadID, "pending")
	require.Len(t, resp.Reminders, 1)
	require.Equal(t, []string{pending.ID}, reminderIDs(resp.Reminders))
}

func TestListReminders_FilterCancelled(t *testing.T) {
	threadID := randomThreadID()
	_ = createTestReminder(t, threadID, 3600, "active "+uuid.NewString())
	cancelled := createTestReminder(t, threadID, 3600, "cancelled "+uuid.NewString())

	cancelResp := postJSON(t, "/cancel-reminder", map[string]string{"reminder_id": cancelled.ID})
	require.Equal(t, http.StatusOK, cancelResp.StatusCode)
	_ = decodeResponse[singleReminderResponse](t, cancelResp)

	resp := listReminders(t, threadID, "cancelled")
	require.Len(t, resp.Reminders, 1)
	require.Equal(t, []string{cancelled.ID}, reminderIDs(resp.Reminders))

}

func TestListReminders_FilterAll(t *testing.T) {
	threadID := randomThreadID()
	first := createTestReminder(t, threadID, 3600, "all "+uuid.NewString())
	second := createTestReminder(t, threadID, 3600, "all "+uuid.NewString())

	cancelResp := postJSON(t, "/cancel-reminder", map[string]string{"reminder_id": second.ID})
	require.Equal(t, http.StatusOK, cancelResp.StatusCode)
	_ = decodeResponse[singleReminderResponse](t, cancelResp)

	resp := listReminders(t, threadID, "all")
	require.Len(t, resp.Reminders, 2)
	require.ElementsMatch(t, []string{first.ID, second.ID}, reminderIDs(resp.Reminders))
}

func TestListReminders_EmptyThread(t *testing.T) {
	resp := listReminders(t, randomThreadID(), "")
	require.Empty(t, resp.Reminders)
	require.NotNil(t, resp.Reminders)
}

func TestListReminders_InvalidThreadID(t *testing.T) {
	resp := postJSON(t, "/list-reminders", map[string]any{
		"thread_id": "bad",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "thread_id must be a valid uuid", body.Error)
}

func TestListReminders_InvalidStatus(t *testing.T) {
	resp := postJSON(t, "/list-reminders", map[string]any{
		"thread_id": randomThreadID(),
		"status":    "bogus",
	})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body := decodeResponse[errorBody](t, resp)
	require.Equal(t, "status must be pending, completed, cancelled, or all", body.Error)
}

func TestListReminders_IsolationBetweenThreads(t *testing.T) {
	threadA := randomThreadID()
	threadB := randomThreadID()
	first := createTestReminder(t, threadA, 3600, "thread A "+uuid.NewString())
	_ = createTestReminder(t, threadB, 3600, "thread B "+uuid.NewString())

	resp := listReminders(t, threadA, "")
	require.Len(t, resp.Reminders, 1)
	require.Equal(t, []string{first.ID}, reminderIDs(resp.Reminders))
}

func listReminders(t *testing.T, threadID, status string) listRemindersResponse {
	t.Helper()
	body := map[string]any{"thread_id": threadID}
	if status != "" {
		body["status"] = status
	}

	resp := postJSON(t, "/list-reminders", body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return decodeResponse[listRemindersResponse](t, resp)
}

func reminderIDs(reminders []reminderResponse) []string {
	ids := make([]string, 0, len(reminders))
	for _, reminder := range reminders {
		ids = append(ids, reminder.ID)
	}
	return ids
}

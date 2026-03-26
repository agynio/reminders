//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	appsv1 "github.com/agynio/reminders/.gen/go/agynio/api/apps/v1"
	threadsv1 "github.com/agynio/reminders/.gen/go/agynio/api/threads/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	remindersAddress string
	threadsAddress   string
	appsAddress      string

	httpClient    *http.Client
	threadsClient threadsv1.ThreadsServiceClient
	appsClient    appsv1.AppsServiceClient

	testIdentityID string
	appIdentityID  string
)

type reminderResponse struct {
	ID          string  `json:"id"`
	ThreadID    string  `json:"thread_id"`
	IdentityID  string  `json:"identity_id"`
	Note        string  `json:"note"`
	Status      string  `json:"status"`
	At          string  `json:"at"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`
	CancelledAt *string `json:"cancelled_at"`
}

type singleReminderResponse struct {
	Reminder reminderResponse `json:"reminder"`
}

type listRemindersResponse struct {
	Reminders []reminderResponse `json:"reminders"`
}

type errorBody struct {
	Error string `json:"error"`
}

func TestMain(m *testing.M) {
	remindersAddress = envOrDefault("REMINDERS_ADDRESS", "reminders:8080")
	threadsAddress = envOrDefault("THREADS_ADDRESS", "threads:50051")
	appsAddress = envOrDefault("APPS_ADDRESS", "apps:50051")
	httpClient = &http.Client{Timeout: 10 * time.Second}
	testIdentityID = uuid.NewString()

	waitForHealthy(remindersAddress, 60*time.Second)

	threadsConn, err := grpc.NewClient(threadsAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: dial threads: %v\n", err)
		os.Exit(1)
	}
	threadsClient = threadsv1.NewThreadsServiceClient(threadsConn)

	appsConn, err := grpc.NewClient(appsAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: dial apps: %v\n", err)
		os.Exit(1)
	}
	appsClient = appsv1.NewAppsServiceClient(appsConn)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	resp, err := appsClient.GetAppBySlug(ctx, &appsv1.GetAppBySlugRequest{Slug: "reminders"})
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: get app by slug: %v\n", err)
		os.Exit(1)
	}
	appIdentityID = resp.GetApp().GetIdentityId()
	if appIdentityID == "" {
		fmt.Fprintf(os.Stderr, "e2e setup: reminders app has no identity_id\n")
		os.Exit(1)
	}

	code := m.Run()

	if err := threadsConn.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e teardown: close threads conn: %v\n", err)
	}
	if err := appsConn.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e teardown: close apps conn: %v\n", err)
	}
	os.Exit(code)
}

func envOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func waitForHealthy(address string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://%s/healthz", address)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(1 * time.Second)
	}
	fmt.Fprintf(os.Stderr, "e2e setup: reminders not healthy within %s\n", timeout)
	os.Exit(1)
}

func postJSON(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	url := fmt.Sprintf("http://%s%s", remindersAddress, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-identity-id", testIdentityID)
	req.Header.Set("x-identity-type", "user")

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	return resp
}

func postJSONNoIdentity(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	url := fmt.Sprintf("http://%s%s", remindersAddress, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	return resp
}

func decodeResponse[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	var payload T
	require.NoError(t, decoder.Decode(&payload))
	return payload
}

func createTestReminder(t *testing.T, threadID string, delaySeconds int64, note string) reminderResponse {
	t.Helper()
	resp := postJSON(t, "/create-reminder", map[string]any{
		"thread_id":     threadID,
		"delay_seconds": delaySeconds,
		"note":          note,
	})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	created := decodeResponse[singleReminderResponse](t, resp)
	return created.Reminder
}

func randomThreadID() string {
	return uuid.NewString()
}

func createThreadWithAppParticipant(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := threadsClient.CreateThread(ctx, &threadsv1.CreateThreadRequest{
		ParticipantIds: []string{testIdentityID, appIdentityID},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.GetThread())

	threadID := resp.GetThread().GetId()
	require.NotEmpty(t, threadID)
	return threadID
}

func getThreadMessages(t *testing.T, threadID string) []*threadsv1.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var all []*threadsv1.Message
	pageToken := ""
	for {
		resp, err := threadsClient.GetMessages(ctx, &threadsv1.GetMessagesRequest{
			ThreadId:  threadID,
			PageSize:  100,
			PageToken: pageToken,
		})
		require.NoError(t, err)
		all = append(all, resp.GetMessages()...)
		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			break
		}
	}
	return all
}

func getReminder(t *testing.T, reminderID string) reminderResponse {
	t.Helper()
	resp := postJSON(t, "/get-reminder", map[string]string{"reminder_id": reminderID})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := decodeResponse[singleReminderResponse](t, resp)
	return body.Reminder
}

func pollReminderStatus(t *testing.T, reminderID, targetStatus string, timeout time.Duration) reminderResponse {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := getReminder(t, reminderID)
		if resp.Status == targetStatus {
			return resp
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("reminder %s did not reach status %q within %s", reminderID, targetStatus, timeout)
	return reminderResponse{}
}

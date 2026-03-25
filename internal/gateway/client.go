package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/google/uuid"
	"github.com/openziti/sdk-golang/ziti"
)

const sendMessagePath = "/agynio.api.gateway.v1.ThreadsGateway/SendMessage"

type SendMessageRequest struct {
	ThreadID string `json:"threadId"`
	SenderID string `json:"senderId"`
	Body     string `json:"body"`
}

type ConnectError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ConnectError) Error() string {
	return fmt.Sprintf("gateway: %s: %s", e.Code, e.Message)
}

type Client struct {
	httpClient    *http.Client
	serviceName   string
	appIdentityID string
}

func NewClient(zitiCtx ziti.Context, serviceName string, appIdentityID string) *Client {
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, addr string) (net.Conn, error) {
			svc := addr
			if host, _, err := net.SplitHostPort(addr); err == nil {
				svc = host
			}
			return zitiCtx.Dial(svc)
		},
	}
	return &Client{
		httpClient:    &http.Client{Transport: transport},
		serviceName:   serviceName,
		appIdentityID: appIdentityID,
	}
}

func (c *Client) SendMessage(ctx context.Context, threadID uuid.UUID, note string) error {
	reqBody := SendMessageRequest{
		ThreadID: threadID.String(),
		SenderID: c.appIdentityID,
		Body:     fmt.Sprintf("Reminder: %s", note),
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	url := fmt.Sprintf("http://%s%s", c.serviceName, sendMessagePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read error response: %w", err)
	}
	var connectErr ConnectError
	if json.Unmarshal(respBody, &connectErr) == nil && connectErr.Code != "" {
		return &connectErr
	}
	return fmt.Errorf("gateway returned status %d: %s", resp.StatusCode, string(respBody))
}

package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Event is the stable operations payload sent to an external receiver.
type Event struct {
	Action   string         `json:"action"`
	Actor    string         `json:"actor,omitempty"`
	HostID   string         `json:"host_id,omitempty"`
	VM       string         `json:"vm,omitempty"`
	JobID    string         `json:"job_id,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
	Occurred time.Time      `json:"occurred"`
}

// Client posts operational events. A nil or unconfigured client is a no-op.
type Client struct {
	url    string
	token  string
	client *http.Client
	log    *slog.Logger
}

func New(url, token string, timeout time.Duration, log *slog.Logger) *Client {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &Client{url: url, token: token, client: &http.Client{Timeout: timeout}, log: log}
}

// Send delivers one event without blocking the caller. Delivery failures are
// logged because the panel action itself has already been accepted.
func (c *Client) Send(event Event) {
	if c == nil || c.url == "" {
		return
	}
	if event.Occurred.IsZero() {
		event.Occurred = time.Now().UTC()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), c.client.Timeout)
		defer cancel()
		body, err := json.Marshal(event)
		if err != nil {
			c.log.Warn("encode webhook", "action", event.Action, "error", err)
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			c.log.Warn("build webhook", "action", event.Action, "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "webvirtcloud-go")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		res, err := c.client.Do(req)
		if err != nil {
			c.log.Warn("send webhook", "action", event.Action, "error", err)
			return
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			c.log.Warn("webhook rejected", "action", event.Action, "status", res.StatusCode)
		}
	}()
}

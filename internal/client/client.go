// Package client is a thin wrapper around the Kemp LoadMaster JSON API
// (POST https://<host>/accessv2).
package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

// DefaultMaxConcurrentRequests caps simultaneous API calls unless Config
// says otherwise. The free LoadMaster rate-limits its API and drops
// connections during the TLS handshake at around eight at once, while
// Terraform runs up to ten operations in parallel by default.
const DefaultMaxConcurrentRequests = 4

// retryDelays are the waits before each retry of a request that never
// reached the LoadMaster.
var retryDelays = []time.Duration{250 * time.Millisecond, time.Second, 3 * time.Second}

// Config holds connection settings for a LoadMaster.
type Config struct {
	Host     string // hostname or IP, optionally with :port
	APIKey   string
	Username string
	Password string
	Insecure bool // skip TLS verification (LoadMasters often use self-signed certs)
	Timeout  time.Duration

	// MaxConcurrentRequests limits simultaneous API calls; 0 means
	// DefaultMaxConcurrentRequests.
	MaxConcurrentRequests int
}

// Client talks to a single LoadMaster.
type Client struct {
	endpoint string
	apiKey   string
	username string
	password string
	http     *http.Client

	// slots limits concurrent requests to Config.MaxConcurrentRequests.
	slots chan struct{}

	// subVSLocks serializes SubVS creation per parent, since the new SubVS
	// is identified by diffing the parent's SubVS list.
	subVSLocksMu sync.Mutex
	subVSLocks   map[int]*sync.Mutex

	// ruleMu serializes rule changes: concurrent rule writes can lose
	// updates on the LoadMaster.
	ruleMu sync.Mutex
}

// New builds a Client from Config.
func New(cfg Config) (*Client, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if cfg.APIKey == "" && (cfg.Username == "" || cfg.Password == "") {
		return nil, fmt.Errorf("either api_key or username and password must be set")
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	host := strings.TrimSuffix(cfg.Host, "/")
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		host = "https://" + host
	}

	maxConcurrent := cfg.MaxConcurrentRequests
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrentRequests
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.Insecure} //nolint:gosec // user opt-in

	return &Client{
		endpoint: host + "/accessv2",
		apiKey:   cfg.APIKey,
		username: cfg.Username,
		password: cfg.Password,
		http:     &http.Client{Timeout: timeout, Transport: transport},
		slots:    make(chan struct{}, maxConcurrent),
	}, nil
}

// APIError is returned when the LoadMaster responds with a non-OK status.
type APIError struct {
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("loadmaster API error (code %d): %s", e.Code, e.Message)
}

// IsNotFound reports whether err is the LoadMaster's response for an object
// that doesn't exist (code 422, e.g. "Unknown VS" or "Rule not found").
func IsNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusUnprocessableEntity {
		return false
	}
	msg := strings.ToLower(apiErr.Message)
	return strings.HasPrefix(msg, "unknown") || strings.Contains(msg, "not found")
}

// baseResponse holds the fields common to every accessv2 response.
type baseResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// Do executes an accessv2 command. params are merged into the request body
// alongside "cmd" and credentials. The full JSON response is decoded into out
// (which may be nil).
func (c *Client) Do(ctx context.Context, cmd string, params map[string]any, out any) error {
	body := map[string]any{"cmd": cmd}
	for k, v := range params {
		body[k] = v
	}
	if c.apiKey != "" {
		body["apikey"] = c.apiKey
	} else {
		body["apiuser"] = c.username
		body["apipass"] = c.password
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	resp, err := c.send(ctx, payload)
	if err != nil {
		return fmt.Errorf("calling %s: %w", cmd, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if !json.Valid(raw) {
		raw = repairJSON(raw)
	}

	var base baseResponse
	if err := json.Unmarshal(raw, &base); err != nil {
		// Auth failures and a disabled API interface return HTML pages, not JSON.
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return &APIError{Code: resp.StatusCode, Message: "authentication failed: check api_key or username/password"}
		case http.StatusNotFound:
			return &APIError{Code: resp.StatusCode, Message: "API not found: check that the API interface is enabled (Certificates & Security > Remote Access)"}
		}
		return fmt.Errorf("decoding %s response (HTTP %d): %w", cmd, resp.StatusCode, err)
	}
	if base.Code != http.StatusOK || !strings.EqualFold(base.Status, "ok") {
		return &APIError{Code: base.Code, Message: base.Message}
	}

	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decoding %s response: %w", cmd, err)
		}
	}
	return nil
}

// send POSTs payload, holding one of the concurrency slots. A request that
// failed before it was written (a refused or dropped connection, or a failed
// TLS handshake) never reached the LoadMaster, so it is retried; once written,
// a failure is returned as-is, since retrying could repeat a write.
func (c *Client) send(ctx context.Context, payload []byte) (*http.Response, error) {
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.slots }()

	for attempt := 0; ; attempt++ {
		var wrote bool
		trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { wrote = true }}
		req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, c.endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err == nil || wrote || attempt == len(retryDelays) || ctx.Err() != nil {
			return resp, err
		}
		select {
		case <-time.After(retryDelays[attempt]):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

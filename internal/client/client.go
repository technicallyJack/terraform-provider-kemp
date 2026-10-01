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
	"strings"
	"time"
)

// Config holds connection settings for a LoadMaster.
type Config struct {
	Host     string // hostname or IP, optionally with :port
	APIKey   string
	Username string
	Password string
	Insecure bool // skip TLS verification (LoadMasters often use self-signed certs)
	Timeout  time.Duration
}

// Client talks to a single LoadMaster.
type Client struct {
	endpoint string
	apiKey   string
	username string
	password string
	http     *http.Client
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

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.Insecure} //nolint:gosec // user opt-in

	return &Client{
		endpoint: host + "/accessv2",
		apiKey:   cfg.APIKey,
		username: cfg.Username,
		password: cfg.Password,
		http:     &http.Client{Timeout: timeout, Transport: transport},
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
// that doesn't exist (code 422, e.g. "Unknown VS").
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusUnprocessableEntity &&
		strings.HasPrefix(strings.ToLower(apiErr.Message), "unknown")
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s: %w", cmd, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
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

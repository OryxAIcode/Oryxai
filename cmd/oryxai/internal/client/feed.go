// Package client is the HTTP client for the OryxAI control plane.
//
// It deliberately uses only the standard library (net/http, encoding/json)
// so the oryxai binary stays tiny — no third-party deps in the install +
// hook hot path.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FeedEvent is the JSON shape the control plane's /feed/ingest
// endpoint accepts. Keep field tags in lockstep with
// internal/control/identity/feed_ingest.go ingestEventInput.
type FeedEvent struct {
	Kind          string `json:"kind"`                    // must start with hook./scan./ci./client.
	TraceID       string `json:"trace_id,omitempty"`
	At            string `json:"at"`                       // RFC3339 UTC
	Tool          string `json:"tool,omitempty"`
	Agent         string `json:"agent,omitempty"`
	ParamsSummary string `json:"params_summary,omitempty"`
}

// ingestRequest is the on-wire batch body.
type ingestRequest struct {
	Events []FeedEvent `json:"events"`
}

// IngestResponse is what the server returns. Accepted+Rejected sum
// to len(events) for a 202.
type IngestResponse struct {
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

// Client posts events to a single org's /feed/ingest endpoint. Reuse
// across goroutines is safe (http.Client is goroutine-safe).
type Client struct {
	ControlURL string        // e.g. "https://oryxai.dev"
	OrgSlug    string        // e.g. "my-team"
	APIKey     string        // csk_live_… — included as Bearer
	HTTP       *http.Client
	UserAgent  string
}

// New constructs a Client with sane defaults. Pass an explicit
// http.Client if you need to tune timeouts further; the default is
// 3 s, which is appropriate for the hook (fire-and-forget) path.
func New(controlURL, orgSlug, apiKey string) *Client {
	return &Client{
		ControlURL: strings.TrimRight(controlURL, "/"),
		OrgSlug:    orgSlug,
		APIKey:     apiKey,
		HTTP: &http.Client{
			Timeout: 3 * time.Second,
		},
		UserAgent: "oryxai-agent",
	}
}

// Ingest sends a batch of events. Empty batches are a no-op.
// On non-2xx responses the body is included in the returned error
// for diagnostic purposes.
func (c *Client) Ingest(ctx context.Context, events []FeedEvent) (*IngestResponse, error) {
	if len(events) == 0 {
		return &IngestResponse{}, nil
	}
	body, err := json.Marshal(ingestRequest{Events: events})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	url := fmt.Sprintf("%s/api/v1/orgs/%s/feed/ingest", c.ControlURL, c.OrgSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ingest http %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}

	var out IngestResponse
	if err := json.Unmarshal(rb, &out); err != nil {
		// 202 with parseable body is the happy path; missing body is
		// also OK (server may eventually return 204) — we still treat
		// as success.
		return &IngestResponse{Accepted: len(events)}, nil
	}
	return &out, nil
}

// Whoami is the parsed response of GET /api/v1/key/whoami. Keep field
// tags in lockstep with internal/control/identity/whoami_key.go
// WhoamiByKeyResponse.
type Whoami struct {
	OrgSlug string `json:"org_slug"`
	OrgName string `json:"org_name"`
	KeyID   string `json:"key_id"`
	KeyName string `json:"key_name"`
}

// WhoamiByKey resolves the API key to its workspace. Used by the
// installer so the user doesn't have to type a slug — the key already
// carries enough identity to find the org server-side.
//
// On 401 the key is invalid/revoked; surface that with a clear
// message rather than a raw HTTP code. Other failures get a generic
// "could not reach control plane" wrap so transient network blips
// don't look like auth errors.
func (c *Client) WhoamiByKey(ctx context.Context) (*Whoami, error) {
	url := fmt.Sprintf("%s/api/v1/key/whoami", c.ControlURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", c.ControlURL, err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
	switch resp.StatusCode {
	case http.StatusOK:
		var out Whoami
		if err := json.Unmarshal(rb, &out); err != nil {
			return nil, fmt.Errorf("whoami: parse response: %w", err)
		}
		if out.OrgSlug == "" {
			return nil, fmt.Errorf("whoami: server returned an empty slug")
		}
		return &out, nil
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("API key not accepted — check that you copied the full csk_… value and that it wasn't revoked")
	case http.StatusNotFound:
		// Older control-plane builds that don't yet ship /key/whoami.
		return nil, errWhoamiUnsupported
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("rate limited by control plane — retry in a few seconds")
	default:
		return nil, fmt.Errorf("whoami http %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
}

// ErrWhoamiUnsupported flags a 404 from /key/whoami so the installer
// can fall back to prompting for the slug instead of failing outright.
// Older control planes (pre-v0.1.1) don't yet ship the endpoint.
var errWhoamiUnsupported = fmt.Errorf("control plane does not support /key/whoami")

// IsWhoamiUnsupported reports whether the error came from a 404 on
// the whoami probe — i.e. an older server build. Callers should fall
// back to asking the user for a slug.
func IsWhoamiUnsupported(err error) bool {
	return err != nil && errors.Is(err, errWhoamiUnsupported)
}

// VerifyKey confirms a (key, slug) pair against the control plane.
// Done by POSTing an empty events batch to /feed/ingest: the bearer
// is validated, the slug→org mismatch check runs, and a 400 (empty
// events) or 202 means everything is wired correctly.
func (c *Client) VerifyKey(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v1/orgs/%s/feed/ingest", c.ControlURL, c.OrgSlug)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader([]byte(`{"events":[]}`)))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("API key not accepted — check that you copied the full csk_… value and that it wasn't revoked")
	case http.StatusForbidden:
		return fmt.Errorf("API key belongs to a different workspace (slug mismatch)")
	case http.StatusNotFound:
		return fmt.Errorf("workspace slug %q not found at %s", c.OrgSlug, c.ControlURL)
	case http.StatusBadRequest, http.StatusAccepted:
		// 400 (empty events array) or 202 — both mean "the bearer
		// authentication path worked; key is valid for this slug".
		return nil
	default:
		return fmt.Errorf("verify http %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
}

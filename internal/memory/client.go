package memory

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type ctxKey int

const requestIDKey ctxKey = 0

// WithRequestID stamps an X-Request-ID onto ctx so the client propagates it.
// Used by callers that already have a correlation ID (e.g. agent-loop run id).
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func requestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok && v != "" {
		return v
	}
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "req-" + hex.EncodeToString(b[:])
}

// Client is the UDS HTTP client for the Kocoro Cloud memory sidecar.
// Per-request timeout applies to the whole exchange; dial uses a 2s
// ctx-cancelable Dialer so a canceled context returns immediately.
type Client struct {
	socket string
	httpc  *http.Client
}

func NewClient(socket string, timeout time.Duration) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 2 * time.Second}
			return d.DialContext(ctx, "unix", socket)
		},
		DisableKeepAlives: false,
		MaxIdleConns:      4,
	}
	return &Client{socket: socket, httpc: &http.Client{Transport: tr, Timeout: timeout}}
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) (int, string, error) {
	rid := requestIDFrom(ctx)
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, rid, fmt.Errorf("%w: marshal: %v", ErrTransport, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, rdr)
	if err != nil {
		return 0, rid, fmt.Errorf("%w: build request: %v", ErrTransport, err)
	}
	req.Header.Set("X-Request-ID", rid)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return 0, rid, fmt.Errorf("%w: %v", ErrTransport, err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, rid, fmt.Errorf("%w: decode: %v", ErrTransport, err)
		}
	}
	return resp.StatusCode, rid, nil
}

// Query returns (env, class, err). Contract:
//   - class is the authoritative branch for the caller. Tool fallback / retry /
//     surface-to-LLM logic should read class only.
//   - err is non-nil for transport-level failures (refused, timeout, EOF, ctx
//     cancel) AND for response-body decode failures on 200. In both cases
//     class == ClassUnavailable and env == nil. The caller may log err for
//     diagnostics but must not branch on it.
//   - For sidecar-reported envelope errors (any non-200), err == nil, env
//     != nil, class is decided by errclass.ClassifyHTTP.
func (c *Client) Query(ctx context.Context, intent QueryIntent) (*ResponseEnvelope, ErrorClass, error) {
	rid := requestIDFrom(ctx)
	req := QueryRequest{Intent: intent, RequestID: &rid}
	var env ResponseEnvelope
	status, _, err := c.do(WithRequestID(ctx, rid), http.MethodPost, "/query", req, &env)
	if err != nil {
		if errors.Is(err, ErrTransport) {
			return nil, ClassUnavailable, err
		}
		return nil, ClassUnavailable, err
	}
	return &env, ClassifyHTTP(status, &env), nil
}

func (c *Client) Reload(ctx context.Context) (*ReloadResponse, error) {
	var r ReloadResponse
	status, _, err := c.do(ctx, http.MethodPost, "/bundle/reload", struct{}{}, &r)
	if err != nil {
		return nil, err
	}
	if status == 409 {
		return &r, fmt.Errorf("reload_in_progress")
	}
	return &r, nil
}

func (c *Client) Health(ctx context.Context) (*HealthPayload, error) {
	var h HealthPayload
	_, _, err := c.do(ctx, http.MethodGet, "/health", nil, &h)
	if err != nil {
		return nil, err
	}
	return &h, nil
}

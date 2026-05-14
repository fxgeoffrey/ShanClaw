// Package uploads is a thin HTTP client for Shannon Cloud's /api/v1/uploads
// endpoints — POST to publish a file, GET to list the current user's still-
// active uploads, DELETE to retract one. POST streams a multipart/form-data
// body via io.Pipe so 50 MiB files never sit in memory in full. All three
// methods classify HTTP responses into typed errors that callers can branch on
// and retry transient failures (5xx + network) with exponential backoff.
package uploads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// UploadResponse mirrors the JSON returned by POST /api/v1/uploads on success.
// Use URL directly — its path segments are already percent-encoded server-side.
type UploadResponse struct {
	URL         string `json:"url"`
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// UploadEntry is a single record in GET /api/v1/uploads. Cloud omits
// s3_key / tenant_id / user_id by design — do not assume they exist.
type UploadEntry struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"created_at"` // RFC3339 UTC
}

// ListResponse mirrors the JSON returned by GET /api/v1/uploads on success.
// TotalCount is the user's active (non-deleted) file count under the current
// filters — it is not "everything they've ever published".
type ListResponse struct {
	Uploads    []UploadEntry `json:"uploads"`
	TotalCount int           `json:"total_count"`
}

// DeleteResponse mirrors the JSON returned by DELETE /api/v1/uploads/{id} on
// success. CDNEvictionSeconds is the worst-case window during which CloudFront
// edge nodes may still serve cached content — surface it to the user so
// they don't think the retract silently failed when the URL "still works"
// for a few minutes.
type DeleteResponse struct {
	Deleted            bool   `json:"deleted"`
	ID                 string `json:"id"`
	CDNEvictionSeconds int    `json:"cdn_eviction_seconds"`
}

// errorBody is the on-the-wire shape of {"error": "...", "message": "..."}.
type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// Sentinel errors. Callers wrap with errors.Is to decide retry policy and how
// to surface the failure to the user.
var (
	// ErrUnauthorized is a 401. Permanent — fix the API key.
	ErrUnauthorized = errors.New("upload: unauthorized")
	// ErrBadRequest is a 400 (missing_file, malformed_multipart). Permanent — client bug.
	ErrBadRequest = errors.New("upload: bad request")
	// ErrEndpointNotFound is a 404. Permanent — the gateway answered but does
	// not have /api/v1/uploads mounted. Usually means the deployment doesn't
	// include the uploads handler yet, or cloud.endpoint points at a wrong host.
	ErrEndpointNotFound = errors.New("upload: endpoint not deployed")
	// ErrFileTooLarge is a 413. Permanent — file exceeds the 50 MiB server limit.
	ErrFileTooLarge = errors.New("upload: file too large")
	// ErrServerConfig is a 500 with code "s3_unconfigured". Permanent — server-side fix needed.
	ErrServerConfig = errors.New("upload: server misconfigured")
	// ErrNotFound is a 404 on the Delete path — the upload id does not exist,
	// has already been retracted, or belongs to another user. Cloud
	// deliberately conflates these three cases to avoid existence leaks, so
	// callers must surface a single "not found or already retracted" message
	// without trying to disambiguate.
	ErrNotFound = errors.New("upload: not found")
	// ErrTransient wraps 500 (other reasons) / 502 / 503 / 504 / network errors.
	// The client retries these internally before returning; once returned, retries
	// have already been exhausted.
	ErrTransient = errors.New("upload: transient")
)

// Client posts files to the Cloud uploads endpoint.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	// retry / backoff knobs (overridable in tests)
	maxAttempts int
	backoff     func(attempt int) time.Duration
}

// NewClient builds a Client. baseURL should be the gateway base (no trailing
// slash, e.g. "https://api-dev.shannon.run"). httpClient is required — pass
// the GatewayClient's existing *http.Client so timeouts and any future tracing
// transport are inherited rather than reinvented.
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 600 * time.Second}
	}
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		httpClient:  httpClient,
		maxAttempts: 3,
		backoff:     defaultBackoff,
	}
}

// defaultBackoff: 1s, 2s, 4s before attempts 2, 3, 4. Attempt 1 has no delay.
func defaultBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	d := time.Second
	for i := 1; i < attempt-1; i++ {
		d *= 2
	}
	return d
}

// Upload streams body to /api/v1/uploads as multipart/form-data and returns
// the parsed response. openBody is a factory: it MUST be cheap to call and
// MUST return a fresh, fully-rewound reader each time, because each retry
// reissues the request and consumes the body afresh.
//
// filename and contentType may be empty. When filename is empty it falls back
// to "upload" (server further falls back to extension sniff on the bytes).
// When contentType is empty the server sniffs by extension or returns
// application/octet-stream.
func (c *Client) Upload(
	ctx context.Context,
	filename, contentType string,
	openBody func() (io.ReadCloser, error),
) (*UploadResponse, error) {
	if openBody == nil {
		return nil, fmt.Errorf("upload: openBody is required")
	}
	return doWithRetry(ctx, c.maxAttempts, c.backoff, func(ctx context.Context) (*UploadResponse, error) {
		return c.uploadOnce(ctx, filename, contentType, openBody)
	})
}

// List calls GET /api/v1/uploads?limit=&offset= and returns the parsed
// response. limit/offset are passed through as-is; cloud clamps limit to
// [1, 100] internally (and 0 → default 20), but callers are encouraged to
// validate before calling so error messages stay close to the user.
func (c *Client) List(ctx context.Context, limit, offset int) (*ListResponse, error) {
	return doWithRetry(ctx, c.maxAttempts, c.backoff, func(ctx context.Context) (*ListResponse, error) {
		return c.listOnce(ctx, limit, offset)
	})
}

// Delete calls DELETE /api/v1/uploads/{id} and returns the parsed response.
// id must be a UUID; the client does not pre-validate format (cloud answers
// 404 for malformed ids, same as for legitimately missing ones).
//
// Delete is idempotent on the server (a second call after a successful first
// returns 404 because deleted_at is non-NULL and the WHERE clause filters the
// row out), so 5xx retries are safe: the worst case is one extra 404 on a
// later retry, which the caller surfaces as "already retracted".
func (c *Client) Delete(ctx context.Context, id string) (*DeleteResponse, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: id is required", ErrBadRequest)
	}
	return doWithRetry(ctx, c.maxAttempts, c.backoff, func(ctx context.Context) (*DeleteResponse, error) {
		return c.deleteOnce(ctx, id)
	})
}

// doWithRetry runs attempt up to maxAttempts times with backoff between
// retries. Only ErrTransient is retried; everything else short-circuits.
// Generic so each method's response type stays statically typed at the call
// site (no any-cast in callers).
func doWithRetry[T any](
	ctx context.Context,
	maxAttempts int,
	backoff func(int) time.Duration,
	attempt func(ctx context.Context) (*T, error),
) (*T, error) {
	var lastErr error
	for n := 1; n <= maxAttempts; n++ {
		if delay := backoff(n); delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err := attempt(ctx)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetriable(err) {
			return nil, err
		}
		if n == maxAttempts {
			break
		}
	}
	return nil, lastErr
}

func isRetriable(err error) bool {
	return errors.Is(err, ErrTransient)
}

// uploadOnce performs a single multipart POST. Streaming is via io.Pipe + a
// goroutine that writes the multipart envelope; the HTTP body is the pipe
// reader so net/http drains it incrementally without buffering the file.
func (c *Client) uploadOnce(
	ctx context.Context,
	filename, contentType string,
	openBody func() (io.ReadCloser, error),
) (*UploadResponse, error) {
	body, err := openBody()
	if err != nil {
		return nil, fmt.Errorf("upload: open body: %w", err)
	}
	defer body.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	// Goroutine writes the multipart envelope into the pipe writer; the HTTP
	// client reads it from the pipe reader as request body.
	go func() {
		defer pw.Close()
		defer mw.Close()

		if contentType != "" {
			if werr := mw.WriteField("content_type", contentType); werr != nil {
				pw.CloseWithError(werr)
				return
			}
		}

		fname := filename
		if fname == "" {
			fname = "upload"
		}
		// Build the file part header manually so we can set Content-Type when
		// the caller specified one (CreateFormFile defaults to octet-stream).
		hdr := make(textproto.MIMEHeader)
		hdr.Set("Content-Disposition",
			fmt.Sprintf(`form-data; name="file"; filename=%q`, fname))
		if contentType != "" {
			hdr.Set("Content-Type", contentType)
		} else {
			hdr.Set("Content-Type", "application/octet-stream")
		}
		part, perr := mw.CreatePart(hdr)
		if perr != nil {
			pw.CloseWithError(perr)
			return
		}
		if _, cerr := io.Copy(part, body); cerr != nil {
			pw.CloseWithError(cerr)
			return
		}
	}()

	endpoint := c.baseURL + "/api/v1/uploads"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil {
		// Unblock the writer goroutine before returning, otherwise it sits
		// forever on the unbuffered pipe write inside io.Copy.
		_ = pr.CloseWithError(err)
		return nil, fmt.Errorf("upload: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network / context-canceled errors. ctx errors propagate as-is so the
		// caller's select can distinguish cancellation from a transport hiccup.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: network: %v", ErrTransient, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out UploadResponse
		if jerr := json.Unmarshal(respBody, &out); jerr != nil {
			return nil, fmt.Errorf("upload: parse response: %w", jerr)
		}
		if out.URL == "" {
			return nil, fmt.Errorf("upload: response missing url field")
		}
		return &out, nil
	}

	return nil, classifyError(resp.StatusCode, respBody, "upload")
}

// listOnce performs a single GET /api/v1/uploads with the given paging.
func (c *Client) listOnce(ctx context.Context, limit, offset int) (*ListResponse, error) {
	endpoint, err := url.Parse(c.baseURL + "/api/v1/uploads")
	if err != nil {
		return nil, fmt.Errorf("upload: build request: %w", err)
	}
	q := endpoint.Query()
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("upload: build request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: network: %v", ErrTransient, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap on list payload

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out ListResponse
		if jerr := json.Unmarshal(respBody, &out); jerr != nil {
			return nil, fmt.Errorf("upload: parse list response: %w", jerr)
		}
		if out.Uploads == nil {
			out.Uploads = []UploadEntry{}
		}
		return &out, nil
	}

	return nil, classifyError(resp.StatusCode, respBody, "list")
}

// deleteOnce performs a single DELETE /api/v1/uploads/{id}.
func (c *Client) deleteOnce(ctx context.Context, id string) (*DeleteResponse, error) {
	endpoint := c.baseURL + "/api/v1/uploads/" + url.PathEscape(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("upload: build request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: network: %v", ErrTransient, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out DeleteResponse
		if jerr := json.Unmarshal(respBody, &out); jerr != nil {
			return nil, fmt.Errorf("upload: parse delete response: %w", jerr)
		}
		return &out, nil
	}

	return nil, classifyError(resp.StatusCode, respBody, "delete")
}

// classifyError maps non-2xx responses to the typed sentinel errors. op
// disambiguates the meaning of 404: "delete" treats 404 as "file not found /
// already retracted / cross-user" (ErrNotFound); all other ops treat 404 as
// "the endpoint isn't deployed at this gateway" (ErrEndpointNotFound). The
// response body is also consulted for the {"error": "..."} code — in
// particular, 500 + s3_unconfigured is a permanent server-config problem,
// while 500 + upload_failed (or no body) is treated as transient.
func classifyError(status int, body []byte, op string) error {
	var parsed errorBody
	_ = json.Unmarshal(body, &parsed)
	code := parsed.Error

	suffix := func() string {
		if parsed.Message != "" {
			return ": " + parsed.Message
		}
		if len(body) > 0 && code == "" {
			s := strings.TrimSpace(string(body))
			if s != "" {
				return ": " + s
			}
		}
		return ""
	}

	switch status {
	case http.StatusUnauthorized: // 401
		return fmt.Errorf("%w (status %d, code %q)%s", ErrUnauthorized, status, code, suffix())
	case http.StatusBadRequest: // 400
		return fmt.Errorf("%w (status %d, code %q)%s", ErrBadRequest, status, code, suffix())
	case http.StatusNotFound: // 404
		if op == "delete" {
			// Cloud returns 404 for: file does not exist / already retracted /
			// belongs to another user / malformed UUID. Do not try to
			// disambiguate — the API deliberately conflates them.
			return fmt.Errorf("%w (status %d)%s", ErrNotFound, status, suffix())
		}
		return fmt.Errorf("%w (status %d)%s", ErrEndpointNotFound, status, suffix())
	case http.StatusRequestEntityTooLarge: // 413
		return fmt.Errorf("%w (status %d, code %q)%s", ErrFileTooLarge, status, code, suffix())
	case http.StatusInternalServerError: // 500
		if code == "s3_unconfigured" {
			return fmt.Errorf("%w (status %d, code %q)%s", ErrServerConfig, status, code, suffix())
		}
		return fmt.Errorf("%w (status %d, code %q)%s", ErrTransient, status, code, suffix())
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout: // 502/503/504
		return fmt.Errorf("%w (status %d, code %q)%s", ErrTransient, status, code, suffix())
	default:
		// Other 4xx → permanent (treat as bad request); other 5xx → transient.
		if status >= 500 {
			return fmt.Errorf("%w (status %d, code %q)%s", ErrTransient, status, code, suffix())
		}
		return fmt.Errorf("%w (status %d, code %q)%s", ErrBadRequest, status, code, suffix())
	}
}

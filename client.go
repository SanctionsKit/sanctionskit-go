package sanctionskit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://www.sanctionskit.com/api/v1"

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_:.-]{8,128}$`)
var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,99}$`)

// Options customizes transport limits. BaseURL defaults to the canonical hosted
// API. Plain HTTP is allowed only for literal loopback hosts or localhost.
// HTTPClient is copied; its redirect policy is always replaced with refusal.
type Options struct {
	BaseURL          string
	HTTPClient       *http.Client
	Timeout          time.Duration
	MaxResponseBytes int64
}

// Client is safe for concurrent use after construction. It does not log requests
// or responses and adds no application-level automatic retries.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	maxBody int64
}

// APIError describes a non-success HTTP response. It intentionally excludes
// arbitrary server messages and bodies, which can contain submitted information.
type APIError struct {
	StatusCode int
	Code       string
	RequestID  string
	RetryAfter string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("sanctionskit: HTTP %d (%s)", e.StatusCode, e.Code)
}

// ClientError reports validation, transport, or response-format failures without
// embedding the key, request URL, submitted subject, or response body.
type ClientError struct {
	Code       string
	StatusCode int
	cause      error
}

func (e *ClientError) Error() string { return "sanctionskit: " + e.Code }

// Unwrap exposes only context cancellation or deadline errors, never a raw
// transport error containing a URL or user data.
func (e *ClientError) Unwrap() error { return e.cause }

// New constructs a client. A zero timeout uses 30 seconds; a zero response limit
// uses 16 MiB. Limits above 120 seconds or 64 MiB are rejected.
func New(apiKey string, options *Options) (*Client, error) {
	if apiKey == "" || strings.TrimSpace(apiKey) != apiKey || strings.ContainsAny(apiKey, "\r\n\t ") {
		return nil, invalidInput()
	}
	for _, character := range apiKey {
		if character < 33 || character > 126 {
			return nil, invalidInput()
		}
	}
	o := Options{}
	if options != nil {
		o = *options
	}
	if o.BaseURL == "" {
		o.BaseURL = defaultBaseURL
	}
	u, err := url.Parse(o.BaseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" {
		return nil, invalidInput()
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, invalidInput()
	}
	if strings.TrimRight(u.Path, "/") != "" && path.Clean(u.Path) != strings.TrimRight(u.Path, "/") {
		return nil, invalidInput()
	}
	if o.Timeout == 0 {
		o.Timeout = 30 * time.Second
	}
	if o.Timeout < 0 || o.Timeout > 120*time.Second {
		return nil, invalidInput()
	}
	if o.MaxResponseBytes == 0 {
		o.MaxResponseBytes = 16 << 20
	}
	if o.MaxResponseBytes < 1 || o.MaxResponseBytes > 64<<20 {
		return nil, invalidInput()
	}
	h := &http.Client{}
	if o.HTTPClient != nil {
		*h = *o.HTTPClient
	}
	h.Timeout = o.Timeout
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: strings.TrimRight(u.String(), "/"), apiKey: apiKey, http: h, maxBody: o.MaxResponseBytes}, nil
}

// CreateScreening screens one subject. Persist idempotencyKey before sending;
// retries of the same business operation must keep its key and body unchanged.
// Requires screenings:write. The server remains authoritative on policy/access.
func (c *Client) CreateScreening(ctx context.Context, input ScreeningRequest, idempotencyKey string) (*Response[ScreeningResult], error) {
	if !keyPattern.MatchString(idempotencyKey) || validateRequest(input) != nil {
		return nil, invalidInput()
	}
	body, err := json.Marshal(input)
	if err != nil || len(body) > 1<<20 {
		return nil, invalidInput()
	}
	raw, requestID, err := c.request(ctx, http.MethodPost, "/screenings", body, idempotencyKey, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	var result ScreeningResult
	data, err := decodeEnvelope(raw, &result)
	if err != nil || !validResult(data, result) {
		return nil, invalidResponse(http.StatusCreated)
	}
	return &Response[ScreeningResult]{Data: result, RequestID: requestID, RawJSON: raw}, nil
}

// GetResult retrieves a retained result, including nullable retained inputs.
// Requires results:read. Expired results return an APIError, often HTTP 410.
func (c *Client) GetResult(ctx context.Context, id string) (*Response[RetainedResult], error) {
	if !uuidPattern.MatchString(id) {
		return nil, invalidInput()
	}
	raw, requestID, err := c.request(ctx, http.MethodGet, "/results/"+id, nil, "", http.StatusOK)
	if err != nil {
		return nil, err
	}
	var result RetainedResult
	data, err := decodeEnvelope(raw, &result)
	if err != nil || !validResult(data, result.ScreeningResult) || !hasFields(data, "subject", "reference") || !strings.EqualFold(result.ID, id) {
		return nil, invalidResponse(http.StatusOK)
	}
	return &Response[RetainedResult]{Data: result, RequestID: requestID, RawJSON: raw}, nil
}

// GetEvidence retrieves the direct JSON evidence document and its exact bytes.
// Requires results:read. It checks the format and result ID before returning it.
func (c *Client) GetEvidence(ctx context.Context, id string) (*EvidenceResponse, error) {
	if !uuidPattern.MatchString(id) {
		return nil, invalidInput()
	}
	raw, requestID, err := c.request(ctx, http.MethodGet, "/results/"+id+"/evidence", nil, "", http.StatusOK)
	if err != nil {
		return nil, err
	}
	var evidence Evidence
	if json.Unmarshal(raw, &evidence) != nil || !hasFields(raw, "format", "result", "retention", "expires_at", "subject", "reference", "request", "retainedInputs", "replayLimit") || evidence.Format != "sanctionskit-evidence@1" || (evidence.Retention != "standard" && evidence.Retention != "minimal") || !strings.EqualFold(evidence.Result.ID, id) {
		return nil, invalidResponse(http.StatusOK)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if !validResult(fields["result"], evidence.Result) || !hasNonNullFields(raw, "format", "result", "retention", "retainedInputs") {
		return nil, invalidResponse(http.StatusOK)
	}
	return &EvidenceResponse{Evidence: evidence, RequestID: requestID, RawJSON: raw}, nil
}

// ListSources retrieves current source capabilities and availability.
// Requires sources:read. The catalog alone does not authorize using a source.
func (c *Client) ListSources(ctx context.Context) (*Response[[]Source], error) {
	raw, requestID, err := c.request(ctx, http.MethodGet, "/sources", nil, "", http.StatusOK)
	if err != nil {
		return nil, err
	}
	var sources []Source
	data, err := decodeEnvelope(raw, &sources)
	if err != nil || sources == nil {
		return nil, invalidResponse(http.StatusOK)
	}
	var records []json.RawMessage
	if json.Unmarshal(data, &records) != nil {
		return nil, invalidResponse(http.StatusOK)
	}
	for i, source := range sources {
		if !hasFields(records[i], "id", "name", "authority", "availability", "capabilities", "rightsStatus", "category") || source.ID == "" || source.Name == "" || source.Authority == "" || source.Availability == "" || source.RightsStatus == "" || source.Category == "" || source.Capabilities == nil {
			return nil, invalidResponse(http.StatusOK)
		}
	}
	return &Response[[]Source]{Data: sources, RequestID: requestID, RawJSON: raw}, nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, body []byte, key string, want int) ([]byte, string, error) {
	if ctx == nil || c == nil || c.http == nil {
		return nil, "", invalidInput()
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", invalidInput()
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sanctionskit-go/0.1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, "", transportError(ctx, err, 0)
	}
	defer res.Body.Close()
	requestID := safeRequestID(res.Header.Get("X-Request-Id"))
	if requestID == c.apiKey {
		requestID = ""
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, c.maxBody+1))
	if err != nil {
		return nil, requestID, transportError(ctx, err, res.StatusCode)
	}
	if int64(len(raw)) > c.maxBody {
		return nil, requestID, &ClientError{Code: "response_too_large", StatusCode: res.StatusCode}
	}
	if res.StatusCode != want {
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return nil, requestID, invalidResponse(res.StatusCode)
		}
		e := &APIError{StatusCode: res.StatusCode, Code: "http_error", RequestID: requestID, RetryAfter: safeRetryAfter(res.Header.Get("Retry-After"))}
		var envelope struct {
			Error struct {
				Code      string `json:"code"`
				RequestID string `json:"requestId"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &envelope) == nil {
			if codePattern.MatchString(envelope.Error.Code) && !strings.Contains(envelope.Error.Code, c.apiKey) {
				e.Code = envelope.Error.Code
			}
			if e.RequestID == "" {
				e.RequestID = safeRequestID(envelope.Error.RequestID)
				if e.RequestID == c.apiKey {
					e.RequestID = ""
				}
			}
		}
		return nil, requestID, e
	}
	mediaType, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || !json.Valid(raw) {
		return nil, requestID, invalidResponse(res.StatusCode)
	}
	return raw, requestID, nil
}

func decodeEnvelope(raw []byte, out any) (json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil || len(envelope["data"]) == 0 || bytes.Equal(envelope["data"], []byte("null")) {
		return nil, invalidResponse(0)
	}
	data := envelope["data"]
	return data, json.Unmarshal(data, out)
}

func hasFields(raw []byte, names ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return false
		}
	}
	return true
}

func validResult(raw []byte, r ScreeningResult) bool {
	if !hasNonNullFields(raw, "id", "environment", "status", "createdAt", "matches", "coverage", "versions", "disclaimer") || !uuidPattern.MatchString(r.ID) || (r.Environment != "sandbox" && r.Environment != "production") || (r.Status != "no_match" && r.Status != "potential_match") || r.CreatedAt == "" || r.Matches == nil || r.Coverage == nil || r.Versions.Dataset == "" || r.Versions.MatchingEngine == "" || r.Versions.Policy == "" {
		return false
	}
	if (r.Status == "no_match" && len(r.Matches) != 0) || (r.Status == "potential_match" && len(r.Matches) == 0) {
		return false
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for _, group := range []struct {
		name     string
		required []string
	}{
		{"matches", []string{"record", "score", "evidence", "conflicts"}},
		{"coverage", []string{"sourceId", "version", "retrievedAt", "fresh"}},
	} {
		var items []json.RawMessage
		if json.Unmarshal(fields[group.name], &items) != nil {
			return false
		}
		for _, item := range items {
			if !hasNonNullFields(item, group.required...) {
				return false
			}
		}
	}
	return true
}

func hasNonNullFields(raw []byte, names ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for _, name := range names {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}

func invalidInput() error { return &ClientError{Code: "invalid_input"} }
func invalidResponse(status int) error {
	return &ClientError{Code: "invalid_response", StatusCode: status}
}
func safeRequestID(id string) string {
	if uuidPattern.MatchString(id) {
		return id
	}
	return ""
}
func safeRetryAfter(value string) string {
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return strconv.FormatUint(seconds, 10)
	}
	if date, err := http.ParseTime(value); err == nil {
		return date.UTC().Format(http.TimeFormat)
	}
	return ""
}
func transportError(ctx context.Context, err error, status int) error {
	if ctx.Err() != nil {
		code := "request_cancelled"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "request_timeout"
		}
		return &ClientError{Code: code, StatusCode: status, cause: ctx.Err()}
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return &ClientError{Code: "request_timeout", StatusCode: status, cause: context.DeadlineExceeded}
	}
	return &ClientError{Code: "network_error", StatusCode: status}
}

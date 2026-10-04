package sanctionskit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const resultID = "00000000-0000-4000-8000-000000000001"
const mockKey = "local-mock-only-secret"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func clientFor(t *testing.T, handler http.HandlerFunc, opts *Options) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	if opts == nil {
		opts = &Options{}
	}
	copy := *opts
	copy.BaseURL = s.URL + "/api/v1"
	c, err := New(mockKey, &copy)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func jsonReply(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Request-Id", resultID)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func validRequest() ScreeningRequest {
	return ScreeningRequest{Subject: Subject{Name: "Alex Morgan", EntityType: "person", BirthDate: "1984"}, Package: "sandbox@1", Retention: "standard", Reference: "example-customer-001"}
}

func TestCreateScreeningTransport(t *testing.T) {
	input := validRequest()
	input.Policy = &PolicyReference{ID: resultID, Version: 3}
	wantBody, _ := json.Marshal(input)
	wantResponse := fixture(t, "screening")
	var calls atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/v1/screenings" || r.URL.RawQuery != "" {
			t.Error("wrong endpoint")
		}
		if r.Header.Get("Authorization") != "Bearer "+mockKey || r.Header.Get("Idempotency-Key") != "customer-operation-001" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("wrong headers")
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Equal(body, wantBody) {
			t.Error("request body changed")
		}
		jsonReply(w, 201, wantResponse)
	}, nil)
	for range 2 {
		response, err := c.CreateScreening(context.Background(), input, "customer-operation-001")
		if err != nil {
			t.Fatal(err)
		}
		if response.Data.Status != "no_match" || response.Data.Versions.Package != "sandbox@1" || response.RequestID != resultID || !bytes.Equal(response.RawJSON, wantResponse) {
			t.Fatal("response was not preserved")
		}
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected retry")
	}
}

func TestGetResultAndEvidence(t *testing.T) {
	retained := fixture(t, "retained")
	evidence := fixture(t, "evidence")
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+mockKey || r.Header.Get("Idempotency-Key") != "" {
			t.Error("wrong read request")
		}
		switch r.URL.Path {
		case "/api/v1/results/" + resultID:
			jsonReply(w, 200, retained)
		case "/api/v1/results/" + resultID + "/evidence":
			jsonReply(w, 200, evidence)
		default:
			t.Error("wrong path")
			w.WriteHeader(404)
		}
	}, nil)
	r, err := c.GetResult(context.Background(), resultID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Data.Subject == nil || r.Data.Subject.Name != "Alex Morgan" || r.Data.Status != "potential_match" || len(r.Data.Matches) != 1 {
		t.Fatal("retained data lost")
	}
	e, err := c.GetEvidence(context.Background(), resultID)
	if err != nil {
		t.Fatal(err)
	}
	if e.Evidence.Result.ID != resultID || !e.Evidence.RetainedInputs || e.Evidence.Request == nil || !bytes.Equal(e.RawJSON, evidence) {
		t.Fatal("raw evidence lost or unwrapped")
	}
}

func TestNullableRetainedInputsAndUnknownEvidence(t *testing.T) {
	var value map[string]any
	_ = json.Unmarshal(fixture(t, "evidence"), &value)
	for _, name := range []string{"subject", "reference", "request", "expires_at"} {
		value[name] = nil
	}
	value["retention"] = "minimal"
	value["retainedInputs"] = false
	value["replayLimit"] = "Inputs were not retained."
	value["futureExtension"] = map[string]any{"publisherField": "preserve this"}
	body, _ := json.MarshalIndent(value, "", "  ")
	body = append(body, '\n')
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { jsonReply(w, 200, body) }, nil)
	e, err := c.GetEvidence(context.Background(), resultID)
	if err != nil {
		t.Fatal(err)
	}
	if e.Evidence.Subject != nil || e.Evidence.Reference != nil || e.Evidence.Request != nil || e.Evidence.ExpiresAt != nil || e.Evidence.RetainedInputs || !bytes.Equal(e.RawJSON, body) {
		t.Fatal("minimal-retention nulls or unknown fields changed")
	}
	var retained map[string]any
	_ = json.Unmarshal(fixture(t, "retained"), &retained)
	data := retained["data"].(map[string]any)
	data["subject"], data["reference"] = nil, nil
	body, _ = json.Marshal(retained)
	r, err := c.GetResult(context.Background(), resultID)
	if err != nil || r.Data.Subject != nil || r.Data.Reference != nil {
		t.Fatal("nullable result failed", err)
	}
}

func TestListSources(t *testing.T) {
	body := fixture(t, "sources")
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/sources" || r.URL.RawQuery != "" {
			t.Error("source query changed")
		}
		jsonReply(w, 200, body)
	}, nil)
	r, err := c.ListSources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Data) != 1 || r.Data[0].ID != "sandbox-synthetic" || r.Data[0].Availability != "available" || len(r.Data[0].Capabilities) != 2 || len(r.Data[0].Qualification) == 0 || !bytes.Equal(r.RawJSON, body) {
		t.Fatal("source details lost")
	}
}

func TestHTTPFailuresDoNotRetryOrExposeBodies(t *testing.T) {
	for _, status := range []int{400, 401, 403, 409, 410, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "7")
				jsonReply(w, status, []byte(`{"error":{"code":"fixture_error","message":"`+mockKey+` Alex Morgan","details":{"subject":"Alex Morgan"}}}`))
			}, nil)
			r, err := c.CreateScreening(context.Background(), validRequest(), "stable-request-001")
			var apiError *APIError
			if r != nil || !errors.As(err, &apiError) || apiError.StatusCode != status || apiError.Code != "fixture_error" || apiError.RetryAfter != "7" || apiError.RequestID != resultID || calls.Load() != 1 {
				t.Fatal("HTTP failure changed", err)
			}
			if strings.Contains(err.Error(), mockKey) || strings.Contains(err.Error(), "Alex Morgan") {
				t.Fatal("sensitive error")
			}
		})
	}
}

func TestRedirectRefusalOverridesCustomClient(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	custom := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { t.Error("custom redirect handler called"); return nil }}
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}, &Options{HTTPClient: custom})
	_, err := c.CreateScreening(context.Background(), validRequest(), "stable-request-001")
	var e *APIError
	if !errors.As(err, &e) || e.StatusCode != 307 || forwarded.Load() != 0 {
		t.Fatal("redirect escaped", err)
	}
	if custom.Timeout != 0 || custom.CheckRedirect == nil {
		t.Fatal("caller client mutated")
	}
}

func TestCancellationAndTimeout(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		name := "timeout"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, &Options{Timeout: 20 * time.Millisecond})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			r, err := c.GetResult(ctx, resultID)
			want := context.DeadlineExceeded
			if cancelled {
				want = context.Canceled
			}
			if r != nil || !errors.Is(err, want) {
				t.Fatal("context semantics lost", err)
			}
		})
	}
}

func TestExpiredContextAndZeroClient(t *testing.T) {
	c := clientFor(t, func(http.ResponseWriter, *http.Request) { t.Error("expired request reached server") }, nil)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := c.ListSources(ctx)
	var e *ClientError
	if !errors.As(err, &e) || e.Code != "request_timeout" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("deadline misclassified", err)
	}
	var zero Client
	if _, err := zero.ListSources(context.Background()); err == nil {
		t.Fatal("zero client accepted")
	}
}

func TestMalformedSuccessfulResponses(t *testing.T) {
	tests := []struct {
		name, body, contentType string
		status                  int
		method                  string
	}{
		{"raw result without envelope", `{}`, "application/json", 200, "result"},
		{"null data", `{"data":null}`, "application/json", 200, "result"},
		{"bad JSON", `{`, "application/json", 200, "result"},
		{"HTML", `<html>not data</html>`, "text/html", 200, "result"},
		{"unexpected success status", `{}`, "application/json", 202, "result"},
		{"evidence envelope", `{"data":{}}`, "application/json", 200, "evidence"},
		{"source object", `{"data":{}}`, "application/json", 200, "sources"},
		{"null sources", `{"data":null}`, "application/json", 200, "sources"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}, nil)
			var err error
			switch tt.method {
			case "result":
				_, err = c.GetResult(context.Background(), resultID)
			case "evidence":
				_, err = c.GetEvidence(context.Background(), resultID)
			case "sources":
				_, err = c.ListSources(context.Background())
			}
			var e *ClientError
			if !errors.As(err, &e) || e.Code != "invalid_response" {
				t.Fatal("bad response accepted", err)
			}
		})
	}
}

func TestResponseLimitAndProxyFailure(t *testing.T) {
	t.Run("limit", func(t *testing.T) {
		c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { jsonReply(w, 200, bytes.Repeat([]byte(" "), 101)) }, &Options{MaxResponseBytes: 100})
		_, err := c.GetResult(context.Background(), resultID)
		var e *ClientError
		if !errors.As(err, &e) || e.Code != "response_too_large" {
			t.Fatal(err)
		}
	})
	t.Run("non JSON error", func(t *testing.T) {
		c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502); _, _ = io.WriteString(w, mockKey) }, nil)
		_, err := c.GetResult(context.Background(), resultID)
		var e *APIError
		if !errors.As(err, &e) || e.StatusCode != 502 || e.Code != "http_error" || strings.Contains(e.Error(), mockKey) {
			t.Fatal(err)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCanonicalDefaultAndSanitizedTransportError(t *testing.T) {
	c, err := New(mockKey, &Options{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://www.sanctionskit.com/api/v1/sources" {
			t.Error("noncanonical default")
		}
		return nil, &url.Error{Op: "Get", URL: "https://example.invalid/" + mockKey, Err: errors.New("private subject")}
	})}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ListSources(context.Background())
	if err == nil || strings.Contains(err.Error(), mockKey) || strings.Contains(err.Error(), "private") || errors.Unwrap(err) != nil {
		t.Fatal("unsafe transport error", err)
	}
}

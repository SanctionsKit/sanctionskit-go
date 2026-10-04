package sanctionskit

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestInvalidInputNeverReachesTransport(t *testing.T) {
	c, err := New(mockKey, &Options{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid input sent")
		return nil, errors.New("unexpected")
	})}})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*ScreeningRequest){
		"short name":       func(r *ScreeningRequest) { r.Subject.Name = "x" },
		"missing selector": func(r *ScreeningRequest) { r.Package = "" },
		"two selectors":    func(r *ScreeningRequest) { r.Sources = []string{"sandbox-synthetic"} },
		"empty source":     func(r *ScreeningRequest) { r.Package = ""; r.Sources = []string{""} },
		"bad date":         func(r *ScreeningRequest) { r.Subject.BirthDate = "2025-02-29" },
		"zero year":        func(r *ScreeningRequest) { r.Subject.BirthDate = "0000" },
		"unknown entity":   func(r *ScreeningRequest) { r.Subject.EntityType = "account" },
		"bad policy":       func(r *ScreeningRequest) { r.Policy = &PolicyReference{ID: resultID, Version: 0} },
		"bad counterparty": func(r *ScreeningRequest) { r.CounterpartyID = "../sources" },
		"bad retention":    func(r *ScreeningRequest) { r.Retention = "forever" },
		"bad identifier":   func(r *ScreeningRequest) { r.Subject.Identifiers = []Identifier{{Value: "value"}} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			input := validRequest()
			change(&input)
			if _, err := c.CreateScreening(context.Background(), input, "stable-request-001"); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	for _, key := range []string{"", "short", "space in key", "key\r\ninjection"} {
		if _, err := c.CreateScreening(context.Background(), validRequest(), key); err == nil {
			t.Fatal("bad key accepted")
		}
	}
	for _, id := range []string{"", "../sources", resultID + "?secret", "not-a-uuid"} {
		if _, err := c.GetResult(context.Background(), id); err == nil {
			t.Fatal("bad result ID")
		}
		if _, err := c.GetEvidence(context.Background(), id); err == nil {
			t.Fatal("bad evidence ID")
		}
	}
	if _, err := c.ListSources(nil); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestDatePrecisionAndSelectors(t *testing.T) {
	for _, date := range []string{"1984", "1984-02", "1984-02-29"} {
		r := validRequest()
		r.Subject.BirthDate = date
		if validateRequest(r) != nil {
			t.Fatal("valid partial date rejected", date)
		}
	}
	r := validRequest()
	r.Package = ""
	r.Sources = []string{"sandbox-synthetic"}
	if validateRequest(r) != nil {
		t.Fatal("explicit sources rejected")
	}
}

func TestInvalidClientOptions(t *testing.T) {
	for _, base := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com?key=x", "https://example.com#x", "https://example.com/a/../b", "https://example.com/%2e%2e", "//example.com", "file:///tmp/example"} {
		if _, err := New(mockKey, &Options{BaseURL: base}); err == nil {
			t.Fatal("unsafe base accepted", base)
		}
	}
	for _, key := range []string{"", "has space", "newline\n"} {
		if _, err := New(key, nil); err == nil {
			t.Fatal("bad key accepted")
		}
	}
	for _, opts := range []Options{{Timeout: -time.Second}, {Timeout: 121 * time.Second}, {MaxResponseBytes: -1}, {MaxResponseBytes: 65 << 20}} {
		if _, err := New(mockKey, &opts); err == nil {
			t.Fatal("bad limit accepted")
		}
	}
}

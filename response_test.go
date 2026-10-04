package sanctionskit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestResultContractFailures(t *testing.T) {
	cases := map[string]func(map[string]any){
		"missing matches":          func(v map[string]any) { delete(v, "matches") },
		"null coverage":            func(v map[string]any) { v["coverage"] = nil },
		"null disclaimer":          func(v map[string]any) { v["disclaimer"] = nil },
		"wrong status":             func(v map[string]any) { v["status"] = "approved" },
		"wrong identity":           func(v map[string]any) { v["id"] = "00000000-0000-4000-8000-000000000002" },
		"absent nullable subject":  func(v map[string]any) { delete(v, "subject") },
		"no-match with candidates": func(v map[string]any) { v["status"] = "no_match" },
		"missing match record":     func(v map[string]any) { delete(v["matches"].([]any)[0].(map[string]any), "record") },
		"null freshness":           func(v map[string]any) { v["coverage"].([]any)[0].(map[string]any)["fresh"] = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var envelope map[string]any
			_ = json.Unmarshal(fixture(t, "retained"), &envelope)
			mutate(envelope["data"].(map[string]any))
			body, _ := json.Marshal(envelope)
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { jsonReply(w, 200, body) }, nil)
			_, err := c.GetResult(context.Background(), resultID)
			var e *ClientError
			if !errors.As(err, &e) || e.Code != "invalid_response" {
				t.Fatal("invalid result accepted", err)
			}
		})
	}
}

func TestEvidenceContractFailures(t *testing.T) {
	cases := map[string]func(map[string]any){
		"wrong format":             func(v map[string]any) { v["format"] = "other" },
		"wrong result ID":          func(v map[string]any) { v["result"].(map[string]any)["id"] = "00000000-0000-4000-8000-000000000002" },
		"missing nullable request": func(v map[string]any) { delete(v, "request") },
		"null retainedInputs":      func(v map[string]any) { v["retainedInputs"] = nil },
		"unknown retention":        func(v map[string]any) { v["retention"] = "forever" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			_ = json.Unmarshal(fixture(t, "evidence"), &value)
			mutate(value)
			body, _ := json.Marshal(value)
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) { jsonReply(w, 200, body) }, nil)
			_, err := c.GetEvidence(context.Background(), resultID)
			var e *ClientError
			if !errors.As(err, &e) || e.Code != "invalid_response" {
				t.Fatal("invalid evidence accepted", err)
			}
		})
	}
}

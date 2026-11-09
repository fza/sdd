package systemone_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
	"github.com/networkteam/sdd/pkg/llm/relevance/systemone"
)

func serve(t *testing.T, handler http.HandlerFunc) *systemone.Scorer {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	scorer, err := systemone.New(systemone.Options{Endpoint: server.URL + "/v3/compat/v1/systemone", APIKey: "test-key", Client: server.Client(), Provider: "opper"})
	if err != nil {
		t.Fatal(err)
	}
	return scorer
}

var request = relevance.Request{Subject: "kind: gap\n\nThe draft text.", Candidates: []relevance.Candidate{
	{ID: "20261101-090000-s-tac-one", Text: "kind: gap\n\nFirst candidate."},
	{ID: "20261101-090000-s-tac-two", Text: "kind: directive\n\nSecond candidate."},
}}

func TestScore_SendsOneNoulQuestionPerCandidateOverASharedState(t *testing.T) {
	var got map[string]any
	scorer := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v3/compat/v1/systemone" {
			t.Errorf("request %s %s should post to the systemone route", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers %v should carry the bearer key and JSON", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		_, _ = io.WriteString(w, `{"model":"jev-1.14.0","answers":{"q0":{"type":"noul","noul":0.74},"q1":{"type":"noul","noul":0.04}},"usage":{"input_tokens":706,"output_tokens":5}}`)
	})
	result, err := scorer.Score(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if got["model"] != systemone.DefaultModel {
		t.Errorf("model %v should default to Jev", got["model"])
	}
	state, _ := got["state"].(map[string]any)
	stateCandidates, _ := state["candidates"].(map[string]any)
	if state["draft"] != request.Subject || stateCandidates["c0"] != request.Candidates[0].Text || stateCandidates["c1"] != request.Candidates[1].Text {
		t.Errorf("state %v should hold the whole draft and every whole candidate", state)
	}
	questions, _ := got["questions"].(map[string]any)
	q1, _ := questions["q1"].(map[string]any)
	if len(questions) != 2 || q1["type"] != "noul" || !strings.Contains(q1["instructions"].(string), "`candidates.c1`") || !strings.Contains(q1["instructions"].(string), "`draft`") {
		t.Errorf("questions %v should ask one noul question per candidate naming its state path", questions)
	}
	if len(result.Scores) != 2 || result.Scores[0] != 0.74 || result.Scores[1] != 0.04 {
		t.Errorf("scores %v should map each answer to its candidate", result.Scores)
	}
	if result.Usage.InputTokens != 706 || result.Usage.OutputTokens != 5 || result.Identity.Model != "jev-1.14.0" || result.Identity.Provider != "opper" {
		t.Errorf("result %+v should carry the reported usage and identity", result)
	}
}

func TestScore_RateLimitReportsRetryAfter(t *testing.T) {
	scorer := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"detail":"rate limit exceeded"}`)
	})
	_, err := scorer.Score(t.Context(), request)
	status, ok := errors.AsType[*systemone.StatusError](err)
	if !ok || status.Status != http.StatusTooManyRequests || status.RetryAfter != 7*time.Second || status.Detail != "rate limit exceeded" {
		t.Fatalf("error %v should be a status error with the retry delay and the detail", err)
	}
	if attributed, ok := errors.AsType[*llm.Error](err); !ok || attributed.Identity.Model != systemone.DefaultModel {
		t.Fatalf("error %v should be attributed to the model", err)
	}
}

func TestScore_ValidationErrorKeepsItsDetail(t *testing.T) {
	scorer := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"detail":[{"loc":["body","state"],"msg":"Field required","type":"missing"}]}`)
	})
	_, err := scorer.Score(t.Context(), request)
	if status, ok := errors.AsType[*systemone.StatusError](err); !ok || !strings.Contains(status.Detail, "Field required") {
		t.Fatalf("error %v should carry the validation detail", err)
	}
}

func TestScore_RefusesMalformedReplies(t *testing.T) {
	for name, body := range map[string]string{
		"not JSON":         `<html>bad gateway</html>`,
		"missing answer":   `{"answers":{"q0":{"type":"noul","noul":0.5}}}`,
		"answer without p": `{"answers":{"q0":{"type":"noul"},"q1":{"type":"noul","noul":0.5}}}`,
		"p outside [0, 1]": `{"answers":{"q0":{"type":"noul","noul":1.5},"q1":{"type":"noul","noul":0.5}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			scorer := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) })
			_, err := scorer.Score(t.Context(), request)
			if _, ok := errors.AsType[*llm.MalformedAnswerError](err); !ok {
				t.Fatalf("error %v should report a malformed answer", err)
			}
		})
	}
}

func TestNew_RefusesMissingKeyAndRelativeEndpoint(t *testing.T) {
	if _, err := systemone.New(systemone.Options{}); err == nil {
		t.Fatal("a scorer without a key should be refused")
	}
	if _, err := systemone.New(systemone.Options{APIKey: "k", Endpoint: "/v1/systemone"}); err == nil {
		t.Fatal("a relative endpoint should be refused")
	}
}

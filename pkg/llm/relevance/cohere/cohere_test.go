package cohere_test

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
	"github.com/networkteam/sdd/pkg/llm/relevance/cohere"
)

func serve(t *testing.T, handler http.HandlerFunc) *cohere.Scorer {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	scorer, err := cohere.New(cohere.Options{Endpoint: server.URL + "/v3/rerank", APIKey: "test-key", Client: server.Client(), Provider: "opper"})
	if err != nil {
		t.Fatal(err)
	}
	return scorer
}

var request = relevance.Request{Subject: "kind: gap\n\nThe draft text.", Candidates: []relevance.Candidate{
	{ID: "20261101-090000-s-tac-one", Text: "kind: gap\n\nFirst candidate."},
	{ID: "20261101-090000-s-tac-two", Text: "kind: directive\n\nSecond candidate."},
}}

func TestScore_RanksEveryCandidateAgainstTheDraftInOneCall(t *testing.T) {
	var got map[string]any
	calls := 0
	scorer := serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/v3/rerank" {
			t.Errorf("request %s %s should post to the rerank route", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers %v should carry the bearer key and JSON", r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		_, _ = io.WriteString(w, `{"id":"r1","model":"aws/cohere/rerank-v3.5","results":[{"index":1,"relevance_score":0.91},{"index":0,"relevance_score":0.02}],"usage":{"total_tokens":42,"cost":0.0004}}`)
	})
	result, err := scorer.Score(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || got["model"] != cohere.DefaultModel {
		t.Errorf("%d calls, model %v: one call should rank the batch with the default model", calls, got["model"])
	}
	query, _ := got["query"].(string)
	documents, _ := got["documents"].([]any)
	if !strings.HasPrefix(query, cohere.Instruction) || !strings.HasSuffix(query, request.Subject) {
		t.Errorf("query %q should be the instruction and the whole draft", query)
	}
	if len(documents) != 2 || documents[0] != request.Candidates[0].Text || documents[1] != request.Candidates[1].Text {
		t.Errorf("documents %v should be the whole candidates in request order", documents)
	}
	if len(result.Scores) != 2 || result.Scores[0] != 0.02 || result.Scores[1] != 0.91 {
		t.Errorf("scores %v should map each ranked index back to its candidate", result.Scores)
	}
	if result.Usage.InputTokens != 42 || result.Usage.CostUSD != 0.0004 || result.Identity.Model != cohere.DefaultModel || result.Identity.Provider != "opper" {
		t.Errorf("result %+v should carry the reported usage and identity", result)
	}
	if scorer.Direction() != relevance.Forward || scorer.MaxBatch() != cohere.MaxBatch || scorer.MaxPairTokens() != cohere.MaxPairTokens {
		t.Error("a reranker scores one direction, up to its batch size, each pair within its window")
	}
}

func TestScore_RateLimitReportsRetryAfter(t *testing.T) {
	scorer := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "9")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"code":"rate_limited","message":"slow down","details":null}}`)
	})
	_, err := scorer.Score(t.Context(), request)
	status, ok := errors.AsType[*cohere.StatusError](err)
	if !ok || status.Status != http.StatusTooManyRequests || status.RetryAfter != 9*time.Second || status.Code != "rate_limited" || status.Message != "slow down" {
		t.Fatalf("error %v should be a status error with the retry delay, code and message", err)
	}
	if attributed, ok := errors.AsType[*llm.Error](err); !ok || attributed.Identity.Model != cohere.DefaultModel {
		t.Fatalf("error %v should be attributed to the model", err)
	}
}

func TestScore_RefusesMalformedReplies(t *testing.T) {
	for name, body := range map[string]string{
		"not JSON":           `<html>bad gateway</html>`,
		"missing candidate":  `{"results":[{"index":0,"relevance_score":0.5}]}`,
		"index out of range": `{"results":[{"index":0,"relevance_score":0.5},{"index":2,"relevance_score":0.5}]}`,
		"index twice":        `{"results":[{"index":0,"relevance_score":0.5},{"index":1,"relevance_score":0.5},{"index":0,"relevance_score":0.4}]}`,
		"no score":           `{"results":[{"index":0},{"index":1,"relevance_score":0.5}]}`,
		"score above one":    `{"results":[{"index":0,"relevance_score":1.5},{"index":1,"relevance_score":0.5}]}`,
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
	if _, err := cohere.New(cohere.Options{}); err == nil {
		t.Fatal("a scorer without a key should be refused")
	}
	if _, err := cohere.New(cohere.Options{APIKey: "k", Endpoint: "/v3/rerank"}); err == nil {
		t.Fatal("a relative endpoint should be refused")
	}
}

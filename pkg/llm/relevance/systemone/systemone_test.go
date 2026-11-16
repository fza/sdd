package systemone_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	scorer, err := systemone.New(systemone.Options{Endpoint: server.URL + "/v3/compat/v1/systemone", APIKey: "test-key", Client: server.Client()})
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
	if result.Usage.InputTokens != 706 || result.Usage.OutputTokens != 5 || result.Identity.Model != "jev-1.14.0" || result.Identity.Provider != "systemone" {
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
		"not JSON":               `<html>bad gateway</html>`,
		"missing answer":         `{"answers":{"q0":{"type":"noul","noul":0.5}}}`,
		"answer without p":       `{"answers":{"q0":{"type":"noul"},"q1":{"type":"noul","noul":0.5}}}`,
		"p outside [0, 1]":       `{"answers":{"q0":{"type":"noul","noul":1.5},"q1":{"type":"noul","noul":0.5}}}`,
		"answer of another type": `{"answers":{"q0":{"type":"bool","noul":0.9},"q1":{"type":"noul","noul":0.5}}}`,
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

// Every planned call fits Jev's limits: the whole request, state and every
// question, within the request limit, and the state beside one question
// within the input limit.
func TestPlan_KeepsEveryCallWithinJevsLimits(t *testing.T) {
	for name, size := range map[string]int{"many short candidates": 150, "a full batch filling the state": 360, "few long candidates": 3000} {
		t.Run(name, func(t *testing.T) {
			var requests, largest, largestState, question int
			scorer := serve(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var got struct {
					State     json.RawMessage            `json:"state"`
					Questions map[string]json.RawMessage `json:"questions"`
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Errorf("request body is not JSON: %v", err)
				}
				requests++
				largest = max(largest, relevance.EstimateTokens(string(body)))
				largestState = max(largestState, relevance.EstimateTokens(string(got.State)))
				answers := map[string]any{}
				for id, q := range got.Questions {
					question = max(question, relevance.EstimateTokens(string(q)))
					answers[id] = map[string]any{"type": "noul", "noul": 0.5}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers})
			})
			candidates := make([]relevance.Candidate, 1000)
			for i := range candidates {
				candidates[i] = relevance.Candidate{ID: strconv.Itoa(i), Text: strings.Repeat("x", size)}
			}
			batches, oversize := relevance.Plan(scorer, "kind: gap\n\nThe draft text.", candidates)
			if len(oversize) > 0 {
				t.Fatalf("%d candidates left out: each fits a call on its own", len(oversize))
			}
			for _, batch := range batches {
				request := relevance.Request{Subject: "kind: gap\n\nThe draft text."}
				for _, i := range batch {
					request.Candidates = append(request.Candidates, candidates[i])
				}
				_, err := scorer.Score(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
			}
			if requests < 2 || largest > systemone.MaxRequestTokens || largestState+question > systemone.MaxInputTokens {
				t.Fatalf("%d calls, largest request %d tokens, largest state %d beside a %d-token question: every call should stay within Jev's limits", requests, largest, largestState, question)
			}
		})
	}
}

// An error page keeps its opening only, marked as cut: the error reaches the
// gather note and the stats.
func TestScore_ExcerptsALongErrorBody(t *testing.T) {
	page := "<html>" + strings.Repeat("bad gateway ", 20_000) + "</html>"
	scorer := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, page)
	})
	_, err := scorer.Score(t.Context(), request)
	status, ok := errors.AsType[*systemone.StatusError](err)
	if !ok || len(err.Error()) > 600 || !strings.HasPrefix(status.Detail, "<html>bad gateway") || !strings.HasSuffix(status.Detail, "(cut)") {
		t.Fatalf("error of %d bytes: a long body should be cut to its opening and marked", len(err.Error()))
	}
}

func TestNew_KeepsEndpointCredentialsOutOfItsError(t *testing.T) {
	for _, endpoint := range []string{"https://user:s3cret@[::1", "//user:s3cret@example.invalid/v1"} {
		_, err := systemone.New(systemone.Options{APIKey: "k", Endpoint: endpoint})
		if err == nil || strings.Contains(err.Error(), "s3cret") {
			t.Errorf("endpoint %q: refused without the password in the error, got %v", endpoint, err)
		}
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

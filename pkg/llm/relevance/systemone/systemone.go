// Package systemone is the relevance adapter over the System One protocol
// (20261109-004506-d-cpt-udz, D22): Jev answers one noul question per
// candidate over a shared state holding the subject and the batch, all in
// one call.
//
//	POST {endpoint}  {"model", "state", "questions": {id: {"type": "noul", "instructions"}}}
//	200              {"model", "answers": {id: {"type": "noul", "noul": p}}, "usage": {"input_tokens", "output_tokens"}}
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// Defaults: Jev through Opper's System One route.
const (
	DefaultEndpoint = "https://api.opper.ai/v3/compat/v1/systemone"
	DefaultModel    = "jev-1.14.0"
	// MaxInputTokens is Jev's limit for state plus the longest question.
	MaxInputTokens = 32_000
	// questionReserve keeps room for the question text in the state limit.
	questionReserve = 400
)

// Question is the relevance question asked of every candidate; %s names the
// candidate's path in the state.
const Question = "Is the entry %s relevant to the entry `draft`? It is relevant when it decides, constrains, contradicts, supersedes, renames, refines, or is required by anything `draft` commits to, or already owns part of the question `draft` answers. Sharing only a topic area or a word is not relevant."

// Options configures a Scorer.
type Options struct {
	// Endpoint is the full URL of the systemone route; empty means
	// DefaultEndpoint.
	Endpoint string
	// Model is the model id; empty means DefaultModel.
	Model  string
	APIKey string
	// Client sends the requests; nil means http.DefaultClient.
	Client *http.Client
	// Provider names the host in reported identities, such as opper.
	Provider string
}

// Scorer rates candidates through one System One call per batch.
type Scorer struct {
	endpoint string
	model    string
	key      string
	client   *http.Client
	identity llm.Identity
}

// New returns a Scorer; the API key is required.
func New(options Options) (*Scorer, error) {
	if options.APIKey == "" {
		return nil, errors.New("systemone: an API key is required")
	}
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || !parsed.IsAbs() {
		return nil, fmt.Errorf("systemone: endpoint %q is not an absolute URL", endpoint)
	}
	model := options.Model
	if model == "" {
		model = DefaultModel
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	provider := options.Provider
	if provider == "" {
		provider = "systemone"
	}
	return &Scorer{endpoint: endpoint, model: model, key: options.APIKey, client: client,
		identity: llm.Identity{Provider: provider, Model: model, Host: parsed.Hostname()}}, nil
}

func (s *Scorer) Direction() relevance.Direction { return relevance.Both }
func (s *Scorer) MaxBatch() int                  { return 0 }
func (s *Scorer) MaxInputTokens() int            { return MaxInputTokens - questionReserve }

type question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

type request struct {
	Model     string              `json:"model"`
	State     map[string]any      `json:"state"`
	Questions map[string]question `json:"questions"`
}

type response struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Type string   `json:"type"`
		Noul *float64 `json:"noul"`
	} `json:"answers"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// StatusError is a non-2xx reply; RetryAfter carries a 429's Retry-After.
type StatusError struct {
	Status     int
	Detail     string
	RetryAfter time.Duration
}

func (e *StatusError) Error() string {
	text := fmt.Sprintf("systemone: HTTP %d", e.Status)
	if e.Detail != "" {
		text += ": " + e.Detail
	}
	if e.RetryAfter > 0 {
		text += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return text
}

func (s *Scorer) Score(ctx context.Context, req relevance.Request) (relevance.Result, error) {
	candidates := map[string]any{}
	questions := map[string]question{}
	for i, candidate := range req.Candidates {
		key := "c" + strconv.Itoa(i)
		candidates[key] = candidate.Text
		questions["q"+strconv.Itoa(i)] = question{Type: "noul", Instructions: fmt.Sprintf(Question, "`candidates."+key+"`")}
	}
	body, err := json.Marshal(request{Model: s.model, State: map[string]any{"draft": req.Subject, "candidates": candidates}, Questions: questions})
	if err != nil {
		return relevance.Result{}, fmt.Errorf("systemone: encoding the request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return relevance.Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.key)
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return relevance.Result{}, s.attribute(fmt.Errorf("systemone: %w", err), llm.Usage{})
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return relevance.Result{}, s.attribute(fmt.Errorf("systemone: reading the reply: %w", err), llm.Usage{})
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return relevance.Result{}, s.attribute(statusError(resp, raw), llm.Usage{})
	}
	var decoded response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return relevance.Result{}, s.attribute(&llm.MalformedAnswerError{Text: string(raw), Err: fmt.Errorf("systemone: unreadable reply: %w", err)}, llm.Usage{})
	}
	usage := llm.Usage{InputTokens: decoded.Usage.InputTokens, OutputTokens: decoded.Usage.OutputTokens}
	identity := s.identity
	if decoded.Model != "" {
		identity.Model = decoded.Model
	}
	result := relevance.Result{Scores: make([]float64, len(req.Candidates)), Identity: identity, Usage: usage}
	for i := range req.Candidates {
		answer, ok := decoded.Answers["q"+strconv.Itoa(i)]
		if !ok || answer.Noul == nil {
			return relevance.Result{}, s.attribute(&llm.MalformedAnswerError{Text: string(raw), Err: fmt.Errorf("systemone: no noul answer for candidate %s", req.Candidates[i].ID)}, usage)
		}
		result.Scores[i] = *answer.Noul
	}
	if err := relevance.CheckResult(req, result); err != nil {
		return relevance.Result{}, s.attribute(&llm.MalformedAnswerError{Text: string(raw), Err: err}, usage)
	}
	return result, nil
}

func (s *Scorer) attribute(err error, usage llm.Usage) error {
	return &llm.Error{Identity: s.identity, Usage: usage, Err: err}
}

func statusError(resp *http.Response, raw []byte) error {
	out := &StatusError{Status: resp.StatusCode}
	var body struct {
		Detail json.RawMessage `json:"detail"`
	}
	if json.Unmarshal(raw, &body) == nil && len(body.Detail) > 0 {
		var text string
		if json.Unmarshal(body.Detail, &text) == nil {
			out.Detail = text
		} else {
			out.Detail = string(body.Detail)
		}
	} else {
		out.Detail = string(bytes.TrimSpace(raw))
	}
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		out.RetryAfter = time.Duration(seconds) * time.Second
	}
	return out
}

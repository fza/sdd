// Package cohere is the relevance adapter over Cohere rerank through
// Opper's rerank route (20261109-004506-d-cpt-udz, D22): one call ranks a
// batch of candidate documents against the draft as the query. A reranker
// reads one direction only, so its score answers only from the draft to each
// candidate.
//
//	POST {endpoint}  {"model", "query", "documents": [text...]}
//	200              {"id", "model", "results": [{"index", "relevance_score"}], "usage": {"total_tokens", "cost"}}
package cohere

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

// Defaults: Cohere rerank v3.5 through Opper.
const (
	DefaultEndpoint = "https://api.opper.ai/v3/rerank"
	DefaultModel    = "aws/cohere/rerank-v3.5"
	// MaxBatch is the documents one rerank call takes.
	MaxBatch = 1000
	// MaxPairTokens is the window rerank reads the query and one document
	// in; a longer pair would be cut, so Plan leaves it to the reader (D30).
	MaxPairTokens = 4096
)

// Instruction prefixes the draft in the query, so the ranking follows the
// relevance a grounding reading asks for.
const Instruction = "Find the existing entries relevant to this draft: entries that decide, constrain, contradict, supersede, rename or refine what it commits to, or already own part of its question. Draft:\n\n"

// Options configures a Scorer.
type Options struct {
	Endpoint string
	Model    string
	APIKey   string
	Client   *http.Client
	Provider string
}

// Scorer ranks candidates through one rerank call per batch.
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
		return nil, errors.New("cohere rerank: an API key is required")
	}
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || !parsed.IsAbs() {
		return nil, fmt.Errorf("cohere rerank: endpoint %q is not an absolute URL", endpoint)
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
		provider = "cohere"
	}
	return &Scorer{endpoint: endpoint, model: model, key: options.APIKey, client: client,
		identity: llm.Identity{Provider: provider, Model: model, Host: parsed.Hostname()}}, nil
}

func (s *Scorer) Direction() relevance.Direction { return relevance.Forward }
func (s *Scorer) MaxBatch() int                  { return MaxBatch }
func (s *Scorer) MaxInputTokens() int            { return 0 }
func (s *Scorer) MaxPairTokens() int             { return MaxPairTokens }

type request struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type response struct {
	Model   string `json:"model"`
	Results []struct {
		Index          *int     `json:"index"`
		RelevanceScore *float64 `json:"relevance_score"`
	} `json:"results"`
	Usage struct {
		TotalTokens int     `json:"total_tokens"`
		Cost        float64 `json:"cost"`
	} `json:"usage"`
}

// StatusError is a non-2xx reply.
type StatusError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *StatusError) Error() string {
	text := fmt.Sprintf("cohere rerank: HTTP %d", e.Status)
	if e.Code != "" {
		text += " " + e.Code
	}
	if e.Message != "" {
		text += ": " + e.Message
	}
	if e.RetryAfter > 0 {
		text += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return text
}

func (s *Scorer) Score(ctx context.Context, req relevance.Request) (relevance.Result, error) {
	documents := make([]string, len(req.Candidates))
	for i, candidate := range req.Candidates {
		documents[i] = candidate.Text
	}
	body, err := json.Marshal(request{Model: s.model, Query: Instruction + req.Subject, Documents: documents})
	if err != nil {
		return relevance.Result{}, fmt.Errorf("cohere rerank: encoding the request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return relevance.Result{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.key)
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return relevance.Result{}, s.attribute(fmt.Errorf("cohere rerank: %w", err), llm.Usage{})
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return relevance.Result{}, s.attribute(fmt.Errorf("cohere rerank: reading the reply: %w", err), llm.Usage{})
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return relevance.Result{}, s.attribute(statusError(resp, raw), llm.Usage{})
	}
	var decoded response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return relevance.Result{}, s.attribute(&llm.MalformedAnswerError{Text: string(raw), Err: fmt.Errorf("cohere rerank: unreadable reply: %w", err)}, llm.Usage{})
	}
	usage := llm.Usage{InputTokens: decoded.Usage.TotalTokens, CostUSD: decoded.Usage.Cost}
	identity := s.identity
	if decoded.Model != "" {
		identity.Model = decoded.Model
	}
	result := relevance.Result{Scores: make([]float64, len(req.Candidates)), Identity: identity, Usage: usage}
	seen := make([]bool, len(req.Candidates))
	for _, ranked := range decoded.Results {
		if ranked.Index == nil || ranked.RelevanceScore == nil || *ranked.Index < 0 || *ranked.Index >= len(req.Candidates) || seen[*ranked.Index] {
			return relevance.Result{}, s.attribute(&llm.MalformedAnswerError{Text: string(raw), Err: errors.New("cohere rerank: a result names no candidate of the request, or one twice")}, usage)
		}
		seen[*ranked.Index] = true
		result.Scores[*ranked.Index] = *ranked.RelevanceScore
	}
	for i, ok := range seen {
		if !ok {
			return relevance.Result{}, s.attribute(&llm.MalformedAnswerError{Text: string(raw), Err: fmt.Errorf("cohere rerank: no result for candidate %s", req.Candidates[i].ID)}, usage)
		}
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
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil && (body.Error.Code != "" || body.Error.Message != "") {
		out.Code, out.Message = body.Error.Code, body.Error.Message
	} else {
		out.Message = string(bytes.TrimSpace(raw))
	}
	if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
		out.RetryAfter = time.Duration(seconds) * time.Second
	}
	return out
}

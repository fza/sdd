package llmops

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

//go:embed relevance_templates/*.tmpl
var relevanceTemplates embed.FS

var relevanceTemplate = template.Must(template.New("relevance").ParseFS(relevanceTemplates, "relevance_templates/*.tmpl"))

// ChatBatch is the most candidates one chat call scores: at 100 a Haiku
// call took 21s at most and always answered every label.
const ChatBatch = 100

// ChatJudge is the prefilter's chat protocol: a chat model scores a list of
// candidates against the draft in one call, each candidate a label and its
// one-line form (relevance.Candidate.Line), entry IDs stripped, so the model
// judges by content and cannot echo an ID back. relevance.Halving shrinks
// the list where an answer breaks.
type ChatJudge struct {
	runner llm.Runner
	system string
}

// NewChatJudge returns the judge over a composed chat runner.
func NewChatJudge(runner llm.Runner) (*ChatJudge, error) {
	var sys strings.Builder
	if err := relevanceTemplate.ExecuteTemplate(&sys, "chat_system", nil); err != nil {
		return nil, fmt.Errorf("rendering the chat prompt: %w", err)
	}
	return &ChatJudge{runner: runner, system: strings.TrimSpace(sys.String())}, nil
}

func (j *ChatJudge) Direction() relevance.Direction { return relevance.Both }
func (j *ChatJudge) MaxBatch() int                  { return ChatBatch }
func (j *ChatJudge) MaxInputTokens() int            { return 0 }
func (j *ChatJudge) MaxPairTokens() int             { return 0 }

// Reads holds for a candidate with a line: the judge reads no entry
// without a summary.
func (j *ChatJudge) Reads(candidate relevance.Candidate) bool { return candidate.Line != "" }

// Score rates every candidate of request in one call.
func (j *ChatJudge) Score(ctx context.Context, request relevance.Request) (relevance.Result, error) {
	n := len(request.Candidates)
	if n > ChatBatch {
		return relevance.Result{}, fmt.Errorf("the chat judge scores at most %d candidates per call, got %d", ChatBatch, n)
	}
	labels := make([]string, n)
	lines := make([]string, n)
	for i, candidate := range request.Candidates {
		if !j.Reads(candidate) {
			return relevance.Result{}, fmt.Errorf("the chat judge reads no candidate without a line, got %s", candidate.ID)
		}
		labels[i] = "c" + strconv.Itoa(i+1)
		lines[i] = labels[i] + " " + withoutEntryIDs(candidate.Line)
	}
	var user strings.Builder
	data := struct {
		Subject string
		Lines   []string
	}{Subject: withoutEntryIDs(request.Subject), Lines: lines}
	if err := relevanceTemplate.ExecuteTemplate(&user, "chat_user", data); err != nil {
		return relevance.Result{}, fmt.Errorf("rendering the chat prompt: %w", err)
	}
	// The candidates are stored entries; the subject is the draft.
	result, err := j.runner.Run(secrets.WithNewParts(ctx, secrets.Part{Where: "draft", Text: request.Subject}), llm.Request{
		Purpose: llm.PurposeRelevance, SystemPrompt: j.system, UserPrompt: strings.TrimSpace(user.String()), ResponseSchema: chatSchema(labels),
	})
	if err != nil {
		return relevance.Result{}, err
	}
	malformed := func(err error) error {
		return &llm.Error{Identity: result.Identity, Usage: result.Usage, Err: &llm.MalformedAnswerError{Text: result.Text, Err: err}}
	}
	text, err := extractJSONObject(result.Text)
	if err != nil {
		return relevance.Result{}, malformed(fmt.Errorf("parsing the chat scores: %w", err))
	}
	var answer struct {
		Scores map[string]float64 `json:"scores"`
	}
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		return relevance.Result{}, malformed(fmt.Errorf("parsing the chat scores: %w", err))
	}
	if len(answer.Scores) != n {
		return relevance.Result{}, malformed(fmt.Errorf("the chat answer holds %d scores for %d candidates", len(answer.Scores), n))
	}
	scores := make([]float64, n)
	for i, label := range labels {
		score, ok := answer.Scores[label]
		if !ok || score < 0 || score > 1 {
			return relevance.Result{}, malformed(fmt.Errorf("the chat answer holds no score between 0 and 1 for %s", label))
		}
		scores[i] = score
	}
	return relevance.Result{Scores: scores, Identity: result.Identity, Usage: result.Usage}, nil
}

// chatSchema holds the answer to an object of one number per label,
// every label required and no other key, the labels in list order.
func chatSchema(labels []string) json.RawMessage {
	var props, required strings.Builder
	for i, label := range labels {
		if i > 0 {
			props.WriteByte(',')
			required.WriteByte(',')
		}
		fmt.Fprintf(&props, `%q:{"type":"number"}`, label)
		fmt.Fprintf(&required, "%q", label)
	}
	return json.RawMessage(`{"type":"object","properties":{"scores":{"type":"object","properties":{` + props.String() +
		`},"required":[` + required.String() + `],"additionalProperties":false}},"required":["scores"],"additionalProperties":false}`)
}

var (
	entryIDPattern = regexp.MustCompile(`\b\d{8}-\d{6}-[a-z]-[a-z]{3}-[a-z0-9]+\b`)
	spaceRun       = regexp.MustCompile(`[ \t]+`)
)

// withoutEntryIDs strips full entry IDs from text, the empty brackets they
// leave and the spaces they double.
func withoutEntryIDs(text string) string {
	text = strings.ReplaceAll(entryIDPattern.ReplaceAllString(text, ""), "()", "")
	return strings.TrimSpace(spaceRun.ReplaceAllString(text, " "))
}

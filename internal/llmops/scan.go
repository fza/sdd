package llmops

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

//go:embed scan_templates/*.tmpl
var scanTemplates embed.FS

var scanTemplate = template.Must(template.New("scan").ParseFS(scanTemplates, "scan_templates/*.tmpl"))

// ChatRevision names the revision of the chat judge's prompt: a hash of its
// template, so a changed prompt reads as a new revision and ends the proofs
// of its hiding rules (model.HidingProof).
func ChatRevision() string {
	data, err := relevanceTemplates.ReadFile("relevance_templates/chat.tmpl")
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:4])
}

// ScanJudge runs the scan after writing (20261121-210755-d-tac-d5e): a chat
// model reads a written entry and the entries a hiding rule hid just below
// its line, and flags those that duplicate or contradict it. It asks
// another question than the ranking judge, so it can disagree with the
// rule. Entry IDs are stripped as the ranking judge strips them.
type ScanJudge struct {
	runner llm.Runner
	system string
}

// NewScanJudge returns the scan over a composed chat runner.
func NewScanJudge(runner llm.Runner) (*ScanJudge, error) {
	var sys strings.Builder
	if err := scanTemplate.ExecuteTemplate(&sys, "scan_system", nil); err != nil {
		return nil, fmt.Errorf("rendering the scan prompt: %w", err)
	}
	return &ScanJudge{runner: runner, system: strings.TrimSpace(sys.String())}, nil
}

// Scan returns the IDs of the hidden entries the model flags against
// written, in the order of hidden, all in one call.
func (j *ScanJudge) Scan(ctx context.Context, written string, hidden []relevance.Candidate) ([]string, error) {
	if len(hidden) == 0 {
		return nil, nil
	}
	labels := make([]string, len(hidden))
	lines := make([]string, len(hidden))
	for i, candidate := range hidden {
		labels[i] = "e" + strconv.Itoa(i+1)
		line := candidate.Line
		if line == "" {
			line = candidate.Text
		}
		lines[i] = labels[i] + " " + withoutEntryIDs(line)
	}
	var user strings.Builder
	data := struct {
		Written string
		Lines   []string
	}{Written: withoutEntryIDs(written), Lines: lines}
	if err := scanTemplate.ExecuteTemplate(&user, "scan_user", data); err != nil {
		return nil, fmt.Errorf("rendering the scan prompt: %w", err)
	}
	result, err := j.runner.Run(secrets.WithNewParts(ctx, secrets.Part{Where: "written entry", Text: written}), llm.Request{
		Purpose: llm.PurposeScan, SystemPrompt: j.system, UserPrompt: strings.TrimSpace(user.String()), ResponseSchema: scanSchema(labels),
	})
	if err != nil {
		return nil, err
	}
	malformed := func(err error) error {
		return &llm.Error{Identity: result.Identity, Usage: result.Usage, Err: &llm.MalformedAnswerError{Text: result.Text, Err: err}}
	}
	text, err := extractJSONObject(result.Text)
	if err != nil {
		return nil, malformed(fmt.Errorf("parsing the scan: %w", err))
	}
	var answer struct {
		Flagged []string `json:"flagged"`
	}
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		return nil, malformed(fmt.Errorf("parsing the scan: %w", err))
	}
	var flagged []string
	for _, label := range answer.Flagged {
		i := slices.Index(labels, label)
		if i < 0 {
			return nil, malformed(fmt.Errorf("the scan flags %q, no label of the list", label))
		}
		if !slices.Contains(flagged, hidden[i].ID) {
			flagged = append(flagged, hidden[i].ID)
		}
	}
	return flagged, nil
}

func scanSchema(labels []string) json.RawMessage {
	quoted := make([]string, len(labels))
	for i, label := range labels {
		quoted[i] = strconv.Quote(label)
	}
	return json.RawMessage(`{"type":"object","properties":{"flagged":{"type":"array","items":{"type":"string","enum":[` + strings.Join(quoted, ",") +
		`]}}},"required":["flagged"],"additionalProperties":false}`)
}

package llmops

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

//go:embed summary_templates/*.tmpl
var summaryTemplates embed.FS

// ErrDescribed is returned by NewDescription for an entry that already holds
// a title and a summary and is not to be regenerated.
var ErrDescribed = errors.New("entry already holds a title and a summary")

// DescriptionResult holds a generated title and, from the paired call, a
// generated summary. A title-only answer leaves Summary empty: the stored
// summary stays as it is, since it may have been corrected by hand.
type DescriptionResult struct {
	Title   string
	Summary string
}

// Description is the one call that writes an entry's title, and its summary
// when the entry needs one (20261107-021219-d-tac-okg). Rendering it is
// separate from running it so a caller can render under its graph lock and
// run the call outside it.
type Description struct {
	Request   llm.Request
	titleOnly bool
}

// NewDescription chooses the call an entry needs: with no summary, or with
// regenerate set, the paired title-and-summary call over the full entry; with
// a summary but no title, the title-only call over the stored summary.
// configuredLanguage is the graph's authoring language locale code (empty =
// English default).
func NewDescription(entry *model.Entry, graph *model.Graph, configuredLanguage string, regenerate bool) (*Description, error) {
	switch {
	case regenerate || entry.Summary == "":
		req, err := renderSummaryTemplates(llm.PurposeSummarize, "summary", summarySchema, newSummaryContext(entry, graph, configuredLanguage))
		if err != nil {
			return nil, fmt.Errorf("rendering summary prompt: %w", err)
		}
		return &Description{Request: req}, nil
	case entry.Title == "":
		req, err := renderSummaryTemplates(llm.PurposeTitle, "title", titleSchema, newTitleContext(entry, graph, configuredLanguage))
		if err != nil {
			return nil, fmt.Errorf("rendering title prompt: %w", err)
		}
		return &Description{Request: req, titleOnly: true}, nil
	default:
		return nil, fmt.Errorf("describing %s: %w", entry.ID, ErrDescribed)
	}
}

// TitleOnly reports whether the call writes the title alone.
func (d *Description) TitleOnly() bool {
	return d.titleOnly
}

// Parse reads the call's answer.
func (d *Description) Parse(output string) (DescriptionResult, error) {
	if d.titleOnly {
		title, err := parseTitleResponse(output)
		return DescriptionResult{Title: title}, err
	}
	return parseSummaryResponse(output)
}

// Describe builds, runs and parses an entry's description in one step.
// Summaries are derived on demand with no staleness tracking (d-cpt-4qi): the
// caller decides when to regenerate.
func Describe(ctx context.Context, runner llm.Runner, entry *model.Entry, graph *model.Graph, configuredLanguage string, regenerate bool) (DescriptionResult, error) {
	description, err := NewDescription(entry, graph, configuredLanguage, regenerate)
	if err != nil {
		return DescriptionResult{}, err
	}
	output, err := runner.Run(ctx, description.Request)
	if err != nil {
		return DescriptionResult{}, fmt.Errorf("running %s call: %w", description.Request.Purpose, err)
	}
	return description.Parse(output.Text)
}

// parseSummaryResponse reads a paired call's answer. Both fields are
// required: a reply without a usable title is as broken as one without a
// summary.
func parseSummaryResponse(output string) (DescriptionResult, error) {
	jsonText, err := extractJSONObject(output)
	if err != nil {
		return DescriptionResult{}, fmt.Errorf("reading summary answer: %w", err)
	}
	var resp struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(jsonText), &resp); err != nil {
		return DescriptionResult{}, fmt.Errorf("parsing summary answer: %w", err)
	}
	title, err := cleanTitle(resp.Title)
	if err != nil {
		return DescriptionResult{}, err
	}
	summary := strings.TrimSpace(resp.Summary)
	if summary == "" {
		return DescriptionResult{}, fmt.Errorf("summary answer carries an empty summary")
	}
	return DescriptionResult{Title: title, Summary: summary}, nil
}

func parseTitleResponse(output string) (string, error) {
	jsonText, err := extractJSONObject(output)
	if err != nil {
		return "", fmt.Errorf("reading title answer: %w", err)
	}
	var resp struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(jsonText), &resp); err != nil {
		return "", fmt.Errorf("parsing title answer: %w", err)
	}
	return cleanTitle(resp.Title)
}

func cleanTitle(raw string) (string, error) {
	title := strings.TrimRight(strings.TrimSpace(raw), ".")
	if title == "" {
		return "", fmt.Errorf("answer carries an empty title")
	}
	if strings.ContainsAny(title, "\r\n") {
		return "", fmt.Errorf("title %q spans more than one line", title)
	}
	return model.CapitalizeTitle(title), nil
}

// summaryContext holds all data needed to render the summary and title
// prompt templates.
type summaryContext struct {
	EntryContent   string
	RelatedEntries string
	// ConfiguredLanguage is the graph authoring language locale code from
	// config (empty = English default). Stable per repo, so the system-prompt
	// prefix stays byte-stable across a graph's captures.
	ConfiguredLanguage string
	StoredSummary      string
	TitleRule          string
}

func newSummaryContext(entry *model.Entry, graph *model.Graph, configuredLanguage string) *summaryContext {
	sctx := &summaryContext{
		EntryContent:       FormatEntryForPrompt(entry),
		ConfiguredLanguage: configuredLanguage,
		TitleRule:          model.TitleRule,
	}

	var parts []string
	forEachRelated(entry, graph, func(relation string, e *model.Entry) {
		// Use summary if available, otherwise full content. Render the
		// layer/kind/type triple (e.g. "strategic insight signal") so the
		// LLM can route on kind — carrying only the bare type would hide
		// whether a referenced signal is a gap, insight, question, etc.
		if e.Summary == "" {
			parts = append(parts, fmt.Sprintf("[%s] %s", relation, FormatEntryForPrompt(e)))
			return
		}
		part := fmt.Sprintf("[%s] %s (ID: %s)\n", relation, entryTriple(e), e.ID)
		if e.Title != "" {
			part += "Title: " + e.Title + "\n"
		}
		parts = append(parts, part+"Summary: "+e.Summary)
	})
	if len(parts) > 0 {
		sctx.RelatedEntries = strings.Join(parts, "\n\n---\n\n")
	}
	return sctx
}

// newTitleContext carries only what a title names: the entry's type, layer
// and kind, its stored summary, and the titles of its direct refs, closes and
// supersedes entries. Neither the body nor any related summary is sent
// (20261107-021219-d-tac-okg).
func newTitleContext(entry *model.Entry, graph *model.Graph, configuredLanguage string) *summaryContext {
	var head strings.Builder
	fmt.Fprintf(&head, "Type: %s\nLayer: %s\n", entry.Type, entry.Layer)
	if entry.Kind != "" {
		fmt.Fprintf(&head, "Kind: %s\n", entry.Kind)
	}
	sctx := &summaryContext{
		EntryContent:       strings.TrimSpace(head.String()),
		ConfiguredLanguage: configuredLanguage,
		StoredSummary:      entry.Summary,
		TitleRule:          model.TitleRule,
	}

	var lines []string
	forEachRelated(entry, graph, func(relation string, e *model.Entry) {
		if e.Title == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("[%s] %s (ID: %s): %s", relation, entryTriple(e), e.ID, e.Title))
	})
	sctx.RelatedEntries = strings.Join(lines, "\n")
	return sctx
}

func forEachRelated(entry *model.Entry, graph *model.Graph, visit func(relation string, related *model.Entry)) {
	seen := make(map[string]bool)
	add := func(ids []string, relation string) {
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			e, ok := graph.ByID[id]
			if !ok {
				continue
			}
			visit(relation, e)
		}
	}
	add(model.RefIDs(entry.Refs), "refs")
	add(entry.Closes, "closes")
	add(entry.Supersedes, "supersedes")
}

func entryTriple(e *model.Entry) string {
	triple := e.LayerLabel()
	if e.Kind != "" {
		triple += " " + string(e.Kind)
	}
	return triple + " " + e.TypeLabel()
}

func renderSummaryTemplates(purpose llm.Purpose, name string, schema json.RawMessage, sctx *summaryContext) (llm.Request, error) {
	tmpl, err := template.ParseFS(summaryTemplates, "summary_templates/*.tmpl")
	if err != nil {
		return llm.Request{}, fmt.Errorf("parsing summary templates: %w", err)
	}

	var sysB, userB strings.Builder
	if err := tmpl.ExecuteTemplate(&sysB, name+"_system", sctx); err != nil {
		return llm.Request{}, fmt.Errorf("executing %s_system template: %w", name, err)
	}
	if err := tmpl.ExecuteTemplate(&userB, name+"_user", sctx); err != nil {
		return llm.Request{}, fmt.Errorf("executing %s_user template: %w", name, err)
	}

	return llm.Request{
		Purpose:        purpose,
		SystemPrompt:   strings.TrimSpace(sysB.String()),
		UserPrompt:     strings.TrimSpace(userB.String()),
		ResponseSchema: schema,
	}, nil
}

// FormatEntryForPrompt formats an entry as readable text for inclusion in a prompt.
//
// Refs rendering is conditional on the entry's ref shape, per d-tac-4ub:
//
//   - All-legacy (every ref has Kind == RefKindUnknown — bare-string YAML
//     fallback): render the flat `Refs: id1, id2` format. Legacy refs carry no
//     kind or desc, so the object form would only add empty `(kind: unknown)`
//     noise.
//   - Object-form (any ref carries a capturable kind): render multi-line
//     `  - id (kind: K): desc` so the LLM sees ref metadata, enabling the
//     ref-meta consistency check (see ref_meta_consistency.tmpl).
//
// Mixed entries (some refs legacy, some object) get the multi-line form —
// the presence of any object-form ref signals an entry authored under the
// new contract; rendering uniformly preserves clarity.
//
// This renders canonical (parse-resolved) ref kinds — the single form both
// pre-flight and summary generation consume.
func FormatEntryForPrompt(e *model.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\n", e.ID)
	fmt.Fprintf(&b, "Type: %s\n", e.Type)
	fmt.Fprintf(&b, "Layer: %s\n", e.Layer)
	if e.Kind != "" {
		fmt.Fprintf(&b, "Kind: %s\n", e.Kind)
	}
	if len(e.Refs) > 0 {
		if allLegacyRefs(e.Refs) {
			fmt.Fprintf(&b, "Refs: %s\n", strings.Join(model.RefIDs(e.Refs), ", "))
		} else {
			b.WriteString("Refs:\n")
			for _, r := range e.Refs {
				fmt.Fprintf(&b, "  - %s (kind: %s)", r.ID, r.Kind)
				if r.Desc != "" {
					fmt.Fprintf(&b, ": %s", r.Desc)
				}
				b.WriteByte('\n')
			}
		}
	}
	if len(e.Closes) > 0 {
		fmt.Fprintf(&b, "Closes: %s\n", strings.Join(e.Closes, ", "))
	}
	if len(e.Supersedes) > 0 {
		fmt.Fprintf(&b, "Supersedes: %s\n", strings.Join(e.Supersedes, ", "))
	}
	if e.Confidence != "" {
		fmt.Fprintf(&b, "Confidence: %s\n", e.Confidence)
	}
	if len(e.Attachments) > 0 {
		fmt.Fprintf(&b, "Attachments: %s\n", strings.Join(e.Attachments, ", "))
	}
	fmt.Fprintf(&b, "\n%s", e.Content)
	return b.String()
}

// allLegacyRefs reports whether every ref in the slice is a legacy bare-string
// ref (Kind == RefKindUnknown). Used by FormatEntryForPrompt to fall back to
// the flat ref rendering for such entries, whose refs carry no kind/desc worth
// showing in object form.
func allLegacyRefs(refs []model.Ref) bool {
	for _, r := range refs {
		if r.Kind != model.RefKindUnknown {
			return false
		}
	}
	return true
}

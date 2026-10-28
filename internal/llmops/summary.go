package llmops

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

//go:embed summary_templates/*.tmpl
var summaryTemplates embed.FS

// SummarizeResult holds the generated title and summary.
type SummarizeResult struct {
	Title   string
	Summary string
}

// Summarize generates a title and a summary for a single entry in one call.
// Summaries are derived on demand with no staleness tracking (d-cpt-4qi), so
// this always regenerates — the caller decides when to invoke it.
// configuredLanguage is the graph's authoring language locale code from config
// (empty = English default); the summary is written in it regardless of the
// source material's language.
func Summarize(ctx context.Context, runner llm.Runner, entry *model.Entry, graph *model.Graph, configuredLanguage string) (*SummarizeResult, error) {
	req, err := RenderSummaryPrompt(entry, graph, configuredLanguage)
	if err != nil {
		return nil, fmt.Errorf("rendering summary prompt: %w", err)
	}

	output, err := runner.Run(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("running summary generator: %w", err)
	}
	return ParseSummaryResponse(output.Text)
}

// ParseSummaryResponse reads a summary call's answer. Both fields are
// required: a reply without a usable title is as broken as one without a
// summary.
func ParseSummaryResponse(output string) (*SummarizeResult, error) {
	jsonText, err := extractJSONObject(output)
	if err != nil {
		return nil, fmt.Errorf("reading summary answer: %w", err)
	}
	var resp summaryResponse
	if err := json.Unmarshal([]byte(jsonText), &resp); err != nil {
		return nil, fmt.Errorf("parsing summary answer: %w", err)
	}
	title, err := cleanTitle(resp.Title)
	if err != nil {
		return nil, err
	}
	summary := strings.TrimSpace(resp.Summary)
	if summary == "" {
		return nil, fmt.Errorf("summary answer carries an empty summary")
	}
	return &SummarizeResult{Title: title, Summary: summary}, nil
}

// GenerateTitle writes a title for an entry that already holds a summary,
// leaving the summary as stored: a summary may have been corrected by hand,
// and nothing records which were.
func GenerateTitle(ctx context.Context, runner llm.Runner, entry *model.Entry, graph *model.Graph, configuredLanguage string) (string, error) {
	req, err := RenderTitlePrompt(entry, graph, configuredLanguage)
	if err != nil {
		return "", fmt.Errorf("rendering title prompt: %w", err)
	}
	output, err := runner.Run(ctx, req)
	if err != nil {
		return "", fmt.Errorf("running title generator: %w", err)
	}
	return ParseTitleResponse(output.Text)
}

// ParseTitleResponse reads a title-only call's answer.
func ParseTitleResponse(output string) (string, error) {
	jsonText, err := extractJSONObject(output)
	if err != nil {
		return "", fmt.Errorf("reading title answer: %w", err)
	}
	var resp titleResponse
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

// summaryContext holds all data needed to render the summary prompt template.
type summaryContext struct {
	EntryContent   string
	RelatedEntries string
	// ConfiguredLanguage is the graph authoring language locale code from
	// config (empty = English default). Stable per repo, so the system-prompt
	// prefix stays byte-stable across a graph's captures.
	ConfiguredLanguage string
	// StoredSummary is the entry's summary as stored, given to a title-only
	// call so the title names what the summary already says.
	StoredSummary string
}

// RenderSummaryPrompt renders the paired title-and-summary prompt for an entry.
func RenderSummaryPrompt(entry *model.Entry, graph *model.Graph, configuredLanguage string) (llm.Request, error) {
	return renderSummaryTemplates(llm.PurposeSummarize, "summary", newSummaryContext(entry, graph, configuredLanguage))
}

// RenderTitlePrompt renders the title-only prompt for an entry that already
// holds a summary.
func RenderTitlePrompt(entry *model.Entry, graph *model.Graph, configuredLanguage string) (llm.Request, error) {
	sctx := newSummaryContext(entry, graph, configuredLanguage)
	sctx.StoredSummary = entry.Summary
	return renderSummaryTemplates(llm.PurposeTitle, "title", sctx)
}

func newSummaryContext(entry *model.Entry, graph *model.Graph, configuredLanguage string) *summaryContext {
	sctx := &summaryContext{
		EntryContent:       FormatEntryForPrompt(entry),
		ConfiguredLanguage: configuredLanguage,
	}

	// Collect direct refs, closes, and supersedes entries.
	var parts []string
	seen := make(map[string]bool)

	addRelated := func(ids []string, relation string) {
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			e, ok := graph.ByID[id]
			if !ok {
				continue
			}
			// Use summary if available, otherwise full content. Render the
			// layer/kind/type triple (e.g. "strategic insight signal") so the
			// LLM can route on kind — carrying only the bare type would hide
			// whether a referenced signal is a gap, insight, question, etc.
			if e.Summary != "" {
				triple := e.LayerLabel()
				if e.Kind != "" {
					triple += " " + string(e.Kind)
				}
				triple += " " + e.TypeLabel()
				part := fmt.Sprintf("[%s] %s (ID: %s)\n", relation, triple, e.ID)
				if e.Title != "" {
					part += "Title: " + e.Title + "\n"
				}
				parts = append(parts, part+"Summary: "+e.Summary)
			} else {
				parts = append(parts, fmt.Sprintf("[%s] %s", relation, FormatEntryForPrompt(e)))
			}
		}
	}

	addRelated(model.RefIDs(entry.Refs), "refs")
	addRelated(entry.Closes, "closes")
	addRelated(entry.Supersedes, "supersedes")

	if len(parts) > 0 {
		sctx.RelatedEntries = strings.Join(parts, "\n\n---\n\n")
	}
	return sctx
}

func renderSummaryTemplates(purpose llm.Purpose, name string, sctx *summaryContext) (llm.Request, error) {
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
		Purpose:      purpose,
		SystemPrompt: strings.TrimSpace(sysB.String()),
		UserPrompt:   strings.TrimSpace(userB.String()),
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

package llmops

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

var preflightLightSchema = mustSchema("preflight_light.json")

var preflightLightExtraction = extraction{
	purpose:        llm.PurposePreflightExtract,
	responseSchema: preflightLightSchema,
	schema:         `{"findings":[{"observation":"...","basis":["<entry id>"],"category":"short-hyphenated-tag","severity":"high|medium|low"}]}`,
	scaleTemplate:  "severity_scale",
}

// LightReading is the grounding reading a light pre-flight judges the draft
// against (20261109-004729-d-cpt-xs5): the entries served in full, the
// entries only listed, in the order the reads listed them, and the entries
// the author set aside with a reason code.
type LightReading struct {
	Full     []string
	Listed   []string
	SetAside []LightSetAside
}

// LightSetAside is one verdict of the author's: entries set aside with a
// reason code.
type LightSetAside struct {
	IDs  []string
	Code string
}

// PreflightLight is the light verifier: pre-flight over the grounding
// reading, adding the axes duplicates, outdated, strain and dismissal. The
// reading is packed into calls of budgetChars characters each, the rules
// and the draft leading every call byte for byte; the calls run in parallel
// and their findings merge into one verdict naming how many calls ran and
// which entries were judged on excerpts.
func PreflightLight(ctx context.Context, runner llm.Runner, entry *model.Entry, graph *model.Graph, configuredLanguage string, reading LightReading, budgetChars int, kindFit *model.KindFit) (*PreflightResult, error) {
	ct := selectCheckType(entry, graph)
	vocab, err := refKindVocabulary(graph)
	if err != nil {
		return nil, err
	}
	pctx := assembleContext(entry, graph, ct, configuredLanguage)
	pctx.RefKindVocabulary = vocab
	pctx.KindFit = kindFit
	base, err := renderPreflightPrompt(ct, pctx)
	if err != nil {
		return nil, fmt.Errorf("rendering pre-flight prompt: %w", err)
	}
	tmpl, err := parsedPreflightTemplates()
	if err != nil {
		return nil, err
	}
	axes := func(partial bool) (string, error) {
		var sb strings.Builder
		err := tmpl.ExecuteTemplate(&sb, "light_axes", struct{ Partial bool }{partial})
		return strings.TrimSpace(sb.String()), err
	}
	rules, err := axes(true)
	if err != nil {
		return nil, fmt.Errorf("rendering the prose verifier axes: %w", err)
	}
	room := budgetChars - len(base.SystemPrompt) - len(base.UserPrompt) - len(rules) - len("\n\n## Reading\n\n")*2
	if room <= 0 {
		return nil, fmt.Errorf("the draft with its rules and referenced entries needs %d characters, more than the %d one prose verifier call holds: raise llm.context_tokens to a larger window, or set verify.mode: deep",
			len(base.SystemPrompt)+len(base.UserPrompt)+len(rules), budgetChars)
	}
	items := lightItems(entry, graph, reading)
	calls, err := model.PlanLightCalls(room, items, overlapRank(entry.Content))
	if err != nil {
		return nil, fmt.Errorf("sizing the prose verifier's pre-flight: %w; raise llm.context_tokens to a larger window, or set verify.mode: deep", err)
	}
	if len(calls) == 0 {
		calls = []model.LightCall{{}}
	}
	if len(calls) == 1 {
		rules, err = axes(false)
		if err != nil {
			return nil, fmt.Errorf("rendering the prose verifier axes: %w", err)
		}
	}
	allowed := map[string]bool{}
	for _, ref := range entry.Refs {
		allowed[ref.ID] = true
	}
	for _, id := range slices.Concat(entry.Closes, entry.Supersedes) {
		allowed[id] = true
	}
	for _, item := range items {
		allowed[item.ID] = true
	}
	results := make([]*PreflightResult, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := base
			req.ResponseSchema = preflightLightSchema
			req.UserPrompt = base.UserPrompt + "\n\n" + rules + "\n\n## Reading\n\n" + renderLightParts(call.Parts)
			results[i], errs[i] = runJSONCheck(ctx, runner, req, preflightLightExtraction, func(output string) (*PreflightResult, error) {
				return parseLightResult(output, allowed)
			})
			if errs[i] == nil {
				results[i].Findings = flagUnlocated(results[i].Findings, req.UserPrompt)
			}
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("running prose verifier pre-flight call %d of %d: %w", i+1, len(calls), err)
		}
	}
	merged := mergeLightResults(results)
	for _, call := range calls {
		for _, part := range call.Parts {
			if part.Excerpted && !slices.Contains(merged.Excerpted, part.ID) {
				merged.Excerpted = append(merged.Excerpted, part.ID)
			}
		}
	}
	merged.Calls = len(calls)
	return merged, nil
}

// lightItems lists the reading as light calls carry it: entries read in
// full with their bodies, then listed ones with their briefs, then set-aside
// ones with their brief and code. Entries the draft refs, closes or
// supersedes already lead every call in full and are left out.
func lightItems(entry *model.Entry, graph *model.Graph, reading LightReading) []model.LightItem {
	seen := map[string]bool{}
	for _, ref := range entry.Refs {
		seen[ref.ID] = true
	}
	for _, id := range slices.Concat(entry.Closes, entry.Supersedes) {
		seen[id] = true
	}
	codes := map[string]string{}
	for _, aside := range reading.SetAside {
		for _, id := range aside.IDs {
			codes[id] = aside.Code
		}
	}
	var items []model.LightItem
	add := func(id string, role model.LightRole) {
		if seen[id] {
			return
		}
		e, ok := graph.ByID[id]
		if !ok {
			return
		}
		seen[id] = true
		if _, aside := codes[id]; aside {
			role = model.LightRoleSetAside
		}
		text := model.EntryBrief(e)
		if role == model.LightRoleFull {
			text = e.Content
		}
		topics := make([]string, 0, len(e.Topics))
		for _, topic := range graph.EffectiveTopics(e) {
			topics = append(topics, topic.String())
		}
		items = append(items, model.LightItem{
			ID: id, Header: ReadingHeader(graph, e, role, codes[id]), Text: text,
			Related: model.RefIDs(e.Refs), Topics: topics,
		})
	}
	for _, id := range reading.Full {
		add(id, model.LightRoleFull)
	}
	for _, id := range reading.Listed {
		add(id, model.LightRoleBrief)
	}
	for _, aside := range reading.SetAside {
		for _, id := range aside.IDs {
			add(id, model.LightRoleSetAside)
		}
	}
	return items
}

// ReadingHeader is the fixed header of one reading entry (V-D13): never cut,
// since status and refs are what outdated and inconsistent findings rest on.
func ReadingHeader(graph *model.Graph, e *model.Entry, role model.LightRole, code string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\n", e.ID)
	fmt.Fprintf(&b, "Type: %s, kind: %s, layer: %s\n", e.Type, e.Kind, e.Layer)
	fmt.Fprintf(&b, "Status: %s\n", derivedStatusForPrompt(graph.DerivedStatus(e)))
	fmt.Fprintf(&b, "Time: %s\n", e.Time.Format("2006-01-02 15:04:05"))
	if topics := graph.EffectiveTopics(e); len(topics) > 0 {
		labels := make([]string, 0, len(topics))
		for _, topic := range topics {
			labels = append(labels, topic.String())
		}
		fmt.Fprintf(&b, "Topics: %s\n", strings.Join(labels, ", "))
	}
	for _, ref := range e.Refs {
		fmt.Fprintf(&b, "Ref: %s %s", ref.Kind, ref.ID)
		if ref.Desc != "" {
			fmt.Fprintf(&b, " (%s)", ref.Desc)
		}
		b.WriteByte('\n')
	}
	if summary := strings.TrimSpace(e.Summary); summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", strings.Join(strings.Fields(summary), " "))
	}
	if role == model.LightRoleSetAside {
		fmt.Fprintf(&b, "Role in the reading: %s (code: %s)\n", role, code)
	} else {
		fmt.Fprintf(&b, "Role in the reading: %s\n", role)
	}
	return b.String()
}

func renderLightParts(parts []model.LightPart) string {
	if len(parts) == 0 {
		return "(the reading holds no entries beyond the referenced ones)"
	}
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		rendered = append(rendered, part.Header+"\n"+part.Text)
	}
	return strings.Join(rendered, "\n\n---\n\n")
}

// overlapRank scores a paragraph by the distinct words of four letters or
// more it shares with the draft, over the root of its length, so a short
// passage on the draft's subject outranks a long one that merely touches it.
func overlapRank(draft string) func(string) float64 {
	vocabulary := map[string]bool{}
	for _, word := range rankWords(draft) {
		vocabulary[word] = true
	}
	return func(paragraph string) float64 {
		words := rankWords(paragraph)
		if len(words) == 0 {
			return 0
		}
		shared := map[string]bool{}
		for _, word := range words {
			if vocabulary[word] {
				shared[word] = true
			}
		}
		return float64(len(shared)) / math.Sqrt(float64(len(words)))
	}
}

func rankWords(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	words := fields[:0]
	for _, field := range fields {
		if len([]rune(field)) >= 4 {
			words = append(words, field)
		}
	}
	return words
}

// parseLightResult parses one light call's answer; a basis ID the prompt
// never carried is dropped, since a light call can only rest on what it
// was served.
func parseLightResult(output string, allowed map[string]bool) (*PreflightResult, error) {
	jsonText, err := extractJSONObject(output)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Findings []struct {
			Severity    string   `json:"severity"`
			Category    string   `json:"category"`
			Observation string   `json:"observation"`
			Quote       string   `json:"quote"`
			Basis       []string `json:"basis"`
		} `json:"findings"`
	}
	err = json.Unmarshal([]byte(jsonText), &resp)
	if err != nil {
		return nil, fmt.Errorf("parsing pre-flight JSON: %w", err)
	}
	if resp.Findings == nil {
		return nil, fmt.Errorf("findings must be a non-null array")
	}
	findings := make([]Finding, 0, len(resp.Findings))
	for i, f := range resp.Findings {
		sev, err := parseSeverity(f.Severity)
		if err != nil {
			return nil, fmt.Errorf("finding %d: %w", i, err)
		}
		if f.Category == "" || f.Observation == "" {
			return nil, fmt.Errorf("finding %d: category and observation must be set", i)
		}
		var basis []string
		for _, id := range f.Basis {
			if allowed[id] && !slices.Contains(basis, id) {
				basis = append(basis, id)
			}
		}
		slices.Sort(basis)
		findings = append(findings, Finding{Severity: sev, Category: f.Category, Observation: f.Observation, Quote: f.Quote, Basis: basis})
	}
	return &PreflightResult{Findings: findings}, nil
}

// mergeLightResults folds the calls' findings into one verdict: findings on
// one category and basis raised by several calls collapse into one, at the
// highest severity raised, naming every call that raised it (V-D11). Within
// one call, every finding stands.
func mergeLightResults(results []*PreflightResult) *PreflightResult {
	if len(results) == 1 {
		return &PreflightResult{Findings: results[0].Findings}
	}
	type merged struct {
		finding Finding
		calls   []int
	}
	var order []string
	byKey := map[string]*merged{}
	for i, result := range results {
		occurrence := map[string]int{}
		for _, finding := range result.Findings {
			key := finding.Category + "\x00" + strings.Join(finding.Basis, ",")
			occurrence[key]++
			key += fmt.Sprintf("\x00%d", occurrence[key])
			m, ok := byKey[key]
			if !ok {
				m = &merged{finding: finding}
				byKey[key] = m
				order = append(order, key)
			} else if severityRank(finding.Severity) > severityRank(m.finding.Severity) {
				m.finding.Severity = finding.Severity
			}
			if !slices.Contains(m.calls, i+1) {
				m.calls = append(m.calls, i+1)
			}
		}
	}
	out := &PreflightResult{Findings: make([]Finding, 0, len(order))}
	for _, key := range order {
		m := byKey[key]
		finding := m.finding
		if len(results) > 1 {
			finding.Observation += fmt.Sprintf(" (raised by prose verifier call %s of %d)", joinInts(m.calls), len(results))
		}
		out.Findings = append(out.Findings, finding)
	}
	return out
}

func severityRank(s Severity) int {
	switch s {
	case SeverityHigh:
		return 2
	case SeverityMedium:
		return 1
	}
	return 0
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ", ")
}

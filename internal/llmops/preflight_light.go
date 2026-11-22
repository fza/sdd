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
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm"
)

var preflightLightSchema = mustSchema("preflight_light.json")

var preflightLightExtraction = extraction{
	purpose:        llm.PurposePreflightExtract,
	responseSchema: withAnalysisFlag(preflightLightSchema),
	schema:         `{"analysis":true,"findings":[{"observation":"...","basis":["<entry id>"],"category":"short-hyphenated-tag","severity":"high|medium|low"}]}`,
	scaleTemplate:  "severity_scale",
}

// LightReading is the grounding reading a light pre-flight judges the draft
// against: the entries served in full, the entries only listed, in the order
// the reads listed them, and the entries the author set aside with a reason
// code.
type LightReading struct {
	Full     []string
	Listed   []string
	SetAside []LightSetAside
	// Shown names the entries the reading showed the author; a listed one
	// left unanswered is implicitly dismissed.
	Shown []string
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
		err := tmpl.ExecuteTemplate(&sb, "light_axes", lightAxes{Partial: partial})
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
	// A call's findings rest only on the entries it was served: the draft's
	// refs, closes and supersedes, and its own reading parts.
	allowedIn := func(call model.LightCall) map[string]bool {
		allowed := map[string]bool{}
		for _, ref := range entry.Refs {
			allowed[ref.ID] = true
		}
		for _, id := range slices.Concat(entry.Closes, entry.Supersedes) {
			allowed[id] = true
		}
		for _, part := range call.Parts {
			allowed[part.ID] = true
		}
		return allowed
	}
	results := make([]*PreflightResult, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		allowed := allowedIn(call)
		go func() {
			defer wg.Done()
			req := base
			req.ResponseSchema = preflightLightSchema
			req.UserPrompt = base.UserPrompt + "\n\n" + rules + "\n\n## Reading\n\n" + renderLightParts(call.Parts)
			results[i], errs[i] = runJSONCheck(secrets.WithNewParts(ctx, secrets.EntryParts(entry)...), runner, req, preflightLightExtraction, func(output string) (*PreflightResult, error) {
				return parseLightResult(output, allowed)
			})
			if errs[i] == nil {
				results[i].Findings = flagKindFitUnnamed(flagUnlocated(results[i].Findings, req.Combined()), kindFit)
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
		} else if role == model.LightRoleBrief && slices.Contains(reading.Shown, id) {
			role = model.LightRoleImplicit
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

// ReadingHeader is the fixed header of one reading entry: never cut,
// since status and refs are what outdated and inconsistent findings rest on.
func ReadingHeader(graph *model.Graph, e *model.Entry, role model.LightRole, code string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\n", e.ID)
	fmt.Fprintf(&b, "Type: %s, kind: %s, layer: %s\n", e.Type, e.Kind, e.Layer)
	fmt.Fprintf(&b, "Status: %s\n", derivedStatusForPrompt(graph.DerivedStatus(e)))
	fmt.Fprintf(&b, "Time: %s\n", e.Time.Format("2006-01-02 15:04:05"))
	writeReadingContext(&b, graph, e)
	writeReadingRole(&b, role, code)
	return b.String()
}

// DeepReadingHeader is ReadingHeader as the deep verifier's reading serves
// it: ID, kind and status share one line, with type, layer and time only
// where the ID does not carry them, and a brief, the role most of its
// entries hold, has no role line.
func DeepReadingHeader(graph *model.Graph, e *model.Entry, role model.LightRole, code string) string {
	var b strings.Builder
	writeDeepFirstLine(&b, e, graph.DerivedStatus(e))
	writeReadingContext(&b, graph, e)
	writeAgentMark(&b, graph, e)
	if role != model.LightRoleBrief {
		writeReadingRole(&b, role, code)
	}
	return b.String()
}

// DeepReadingBlock is e's block of the deep verifier's reading, served in
// full: its DeepReadingHeader, then its body.
func DeepReadingBlock(graph *model.Graph, e *model.Entry, role model.LightRole, code string) model.DeepReadingBlock {
	return model.DeepReadingBlock{ID: e.ID, Text: "---\n\n" + DeepReadingHeader(graph, e, role, code) + "\n" + e.Content}
}

// deepStatus is derivedStatusForPrompt with a done entry's status in one
// word: its line names the kind already.
func deepStatus(s model.Status) string {
	if s.Kind == model.StatusNone {
		return "terminal"
	}
	return derivedStatusForPrompt(s)
}

func writeDeepFirstLine(b *strings.Builder, e *model.Entry, status model.Status) {
	parts, err := model.ParseID(e.ID)
	if err == nil && model.TypeFromAbbrev[parts.TypeCode] == e.Type && model.LayerFromAbbrev[parts.LayerCode] == e.Layer &&
		e.Time.Format("20060102-150405") == parts.Timestamp {
		fmt.Fprintf(b, "ID: %s | kind: %s | Status: %s\n", e.ID, e.Kind, deepStatus(status))
		return
	}
	fmt.Fprintf(b, "ID: %s | Type: %s, kind: %s, layer: %s | Status: %s | Time: %s\n", e.ID, e.Type, e.Kind, e.Layer, deepStatus(status), e.Time.Format("2006-01-02 15:04:05"))
}

// writeAgentMark states who decided e when an agent did, and where the
// unapproved agent reasoning it rests on starts.
func writeAgentMark(b *strings.Builder, graph *model.Graph, e *model.Entry) {
	if e.AgentDecided() {
		fmt.Fprintf(b, "Decided by: an agent alone, run by %s\n", e.RunBy)
	}
	writeApproval(b, e)
	if mark := graph.AgentMark(e.ID); mark.Hops > 0 {
		roots := make([]string, 0, len(mark.Roots))
		for _, root := range mark.Roots {
			roots = append(roots, model.ShortID(root))
		}
		fmt.Fprintf(b, "Agent-reasoned via %s, %d step(s) back: no person approved the agent reasoning it rests on\n", strings.Join(roots, " and "), mark.Hops)
	}
}

// writeApproval states who approved e (model.Entry.Approved) and who stated
// its reason and a directive's intent.
func writeApproval(b *strings.Builder, e *model.Entry) {
	if !e.Approved() {
		return
	}
	fmt.Fprintf(b, "Approved by: %s, drafted by an agent\n", e.ApprovedBy)
	if e.Reason != "" {
		stated := "given by " + e.ReasonBy
		if e.ReasonBy == model.DraftedByAgent {
			stated = "drafted by the agent"
		}
		fmt.Fprintf(b, "Reason, %s: %s\n", stated, e.Reason)
	}
	if e.IntentBy != "" {
		by := e.IntentBy
		if by == model.DraftedByAgent {
			by = "the agent"
		}
		fmt.Fprintf(b, "Intent set by: %s\n", by)
	}
}

func writeReadingContext(b *strings.Builder, graph *model.Graph, e *model.Entry) {
	if topics := graph.EffectiveTopics(e); len(topics) > 0 {
		labels := make([]string, 0, len(topics))
		for _, topic := range topics {
			labels = append(labels, topic.String())
		}
		fmt.Fprintf(b, "Topics: %s\n", strings.Join(labels, ", "))
	}
	for _, ref := range e.Refs {
		fmt.Fprintf(b, "Ref: %s %s", ref.Kind, ref.ID)
		if ref.Desc != "" {
			fmt.Fprintf(b, " (%s)", ref.Desc)
		}
		b.WriteByte('\n')
	}
	if summary := strings.TrimSpace(e.Summary); summary != "" {
		fmt.Fprintf(b, "Summary: %s\n", strings.Join(strings.Fields(summary), " "))
	}
}

func writeReadingRole(b *strings.Builder, role model.LightRole, code string) {
	if role == model.LightRoleSetAside {
		fmt.Fprintf(b, "Role in the reading: %s (code: %s)\n", role, code)
	} else {
		fmt.Fprintf(b, "Role in the reading: %s\n", role)
	}
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

// mergeLightResults folds the calls' findings into one verdict: one
// observation on one category and basis raised by several calls collapses
// into one, at the highest severity raised, naming every call that raised
// it. Within one call, every finding stands.
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
			// The draft's refs lead every call, so a shared basis alone does not
			// make two observations one finding.
			key := finding.Category + "\x00" + strings.Join(finding.Basis, ",") + "\x00" + strings.ToLower(strings.TrimSpace(finding.Observation))
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

// DeepListedID names the deep reading's closing block, which lists the
// entries the reading does not serve.
const DeepListedID = "the entries the reading lists without serving them"

// DeepListedLine is one entry of the deep reading's closing block: Summary
// lists it by title and first summary sentence, else by title; Hidden marks
// it scored below the prefilter's threshold, Implicit dismissed implicitly.
// Dismissed is the code of the drafter's verdict on the entry; such a line
// shows the judge's Score (nil: not judged) and the entry's Status.
type DeepListedLine struct {
	Entry     *model.Entry
	Summary   bool
	Hidden    bool
	Implicit  bool
	Dismissed string
	Score     *float64
	Status    model.Status
}

// DeepRankingNote heads the verifier's reading with what ranked the entries
// placed by rank, with counts, and the doubled tiers of a widened reading;
// empty when the rank placed none.
func DeepRankingNote(sources model.RankingSources, judge string, widened bool) string {
	var b strings.Builder
	b.WriteString("\n## Ranking\n\nRanking source: ")
	switch sources.Kind() {
	case "":
		return ""
	case "judge":
		fmt.Fprintf(&b, "judge %s, %d entries.", judge, sources.Judged)
	case "similarity":
		fmt.Fprintf(&b, "similarity to the draft, %d entries: no judge scored them.", sources.Similar)
	case "listed order":
		fmt.Fprintf(&b, "listed order, %d entries: no judge scored them and no embedding ranked them.", sources.Listed)
	default:
		var parts []string
		if sources.Judged > 0 {
			parts = append(parts, fmt.Sprintf("%d by judge %s", sources.Judged, judge))
		}
		if sources.Similar > 0 {
			parts = append(parts, fmt.Sprintf("%d by similarity to the draft", sources.Similar))
		}
		if sources.Listed > 0 {
			parts = append(parts, fmt.Sprintf("%d in listed order", sources.Listed))
		}
		fmt.Fprintf(&b, "mixed, %s.", strings.Join(parts, ", "))
	}
	if widened {
		fmt.Fprintf(&b, " The judge's ranking recall is below the bar, so the reading serves the top %d in full and lists the next %d with their summary.", 2*model.DeepTopRanked, 2*model.DeepSummaryRanked)
	}
	b.WriteString("\n")
	return b.String()
}

// DeepListedBlock lists the entries the reading does not serve, one line
// each in groups: the drafter's dismissals, the implicit dismissals, those
// below the prefilter's threshold, then the others, each under its heading
// and in reading order.
// It ends with the count of those past a listing ceiling. Every listed and
// counted line of the reading is rendered here.
func DeepListedBlock(lines []DeepListedLine, counted int) model.DeepReadingBlock {
	var b strings.Builder
	fmt.Fprintf(&b, "---\n\n## Listed, not served (%d)\n\nThe reading ranks these below the entries it serves: one line each, ID and title, the higher-ranked ones with the first sentence of their summary, in rank order within each group. An entry the drafter dismissed carries the judge's score (`?`: not judged), the code of the drafter's verdict and its status: open it by ID to audit the dismissal. An entry below the prefilter's threshold is one its calibrated judge scored below the threshold its recall bound holds at. Open any of them with show when it bears on the draft; a finding may rest on it.\n", len(lines))
	var dismissed, implicit, hidden, other []string
	for _, line := range lines {
		e := line.Entry
		if line.Dismissed != "" {
			dismissed = append(dismissed, deepDismissedLine(line))
			continue
		}
		text := model.EntryBrief(e)
		if sentence := e.FirstSummarySentence(); line.Summary && e.Title != "" && sentence != "" {
			text = e.Title + ": " + sentence
		}
		text = e.ID + "  " + text
		switch {
		case line.Implicit:
			implicit = append(implicit, text)
		case line.Hidden:
			hidden = append(hidden, text)
		default:
			other = append(other, text)
		}
	}
	headed := len(dismissed) > 0 || len(implicit) > 0 || len(hidden) > 0
	var groups []string
	for _, group := range []struct {
		heading string
		lines   []string
	}{
		{"Dismissed by the drafter:", dismissed},
		{"Shown to the capture and left unanswered (" + string(model.LightRoleImplicit) + "):", implicit},
		{"Below the prefilter's threshold:", hidden},
		{"Other listed entries:", other},
	} {
		if len(group.lines) == 0 {
			continue
		}
		text := strings.Join(group.lines, "\n")
		if headed {
			text = group.heading + "\n" + text
		}
		groups = append(groups, text)
	}
	if counted > 0 {
		groups = append(groups, fmt.Sprintf("%d more listed, open with search or show", counted))
	}
	if len(groups) > 0 {
		b.WriteString("\n" + strings.Join(groups, "\n\n"))
	}
	return model.DeepReadingBlock{ID: DeepListedID, Text: b.String()}
}

// deepDismissedLine is an entry the drafter dismissed, in the line format of
// the verifier's start (20261121-223938-d-tac-rqi): the score column, the
// drafter's verdict, ID, status and title, then the first summary sentence.
func deepDismissedLine(line DeepListedLine) string {
	e := line.Entry
	score := "?  "
	if line.Score != nil {
		score = strings.TrimPrefix(fmt.Sprintf("%.2f", *line.Score), "0")
	}
	status := ""
	if line.Status.Kind != model.StatusNone {
		status = " {" + string(line.Status.Kind)
		if line.Status.By != "" {
			status += " " + line.Status.By
		}
		status += "}"
	}
	text := fmt.Sprintf("  %s drafter: dismissed, %s  %s%s  %s", score, line.Dismissed, e.ID, status, e.ListingTitle())
	if sentence := e.FirstSummarySentence(); e.Title != "" && sentence != "" {
		text += "\n      " + sentence
	}
	return text
}

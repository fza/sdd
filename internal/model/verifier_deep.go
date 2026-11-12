package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The deep verifier exchange (20261109-004729-d-cpt-xs5, V-D1, V-D2, V-D25):
// the verifier returns findings with per-finding search-replace edits over the
// base draft document, the drafter accepts or contests each, the verifier
// answers the contested ones, and sdd builds the proposal, its hunks and the
// finding each hunk carries.

// DeepRounds is how many deliveries a deep verifier makes before the open
// points go to the user.
const DeepRounds = 3

// DeepRevisions is how many revisions of a draft one deep verifier judges
// before a fresh one starts; each opens DeepRounds deliveries of its own.
const DeepRevisions = 3

// DeepPageChars bounds one served page of the deep verifier's reading (V-D8).
const DeepPageChars = 50000

var deepAxes = []string{"contradicts", "duplicates", "outdated", "inconsistent", "ref-fit", "missing-ref", "dismissal", "aspiration", "strain", "evidence"}

// DeepAxes lists the closed set of axes a deep finding names.
func DeepAxes() []string { return slices.Clone(deepAxes) }

// DeepEdit is one exact replacement over the base draft document.
type DeepEdit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// DeepPointer locates the piece of the draft a finding judges: a quote from a
// field, or an entry the field names.
type DeepPointer struct {
	Field string `json:"field"`
	Quote string `json:"quote,omitempty"`
	ID    string `json:"id,omitempty"`
}

// DeepFinding is one finding of the deep verifier, reasoning first.
type DeepFinding struct {
	Verdict  string        `json:"verdict"`
	Axis     string        `json:"axis"`
	Severity string        `json:"severity"`
	Owner    bool          `json:"owner,omitempty"`
	Pointers []DeepPointer `json:"pointers,omitempty"`
	Basis    []string      `json:"basis,omitempty"`
	Sources  []string      `json:"sources,omitempty"`
	Edits    []DeepEdit    `json:"edits,omitempty"`
}

// Validate checks the finding's shape: the closed axis and severity sets, an
// owner finding without edits, evidence with sources, and full entry IDs.
func (f DeepFinding) Validate() error {
	if strings.TrimSpace(f.Verdict) == "" {
		return errors.New("verdict must say in words what is wrong")
	}
	if !slices.Contains(deepAxes, f.Axis) {
		return fmt.Errorf("axis %q is not one of %s", f.Axis, strings.Join(deepAxes, ", "))
	}
	if f.Severity != "high" && f.Severity != "medium" && f.Severity != "low" {
		return fmt.Errorf("severity %q is not high, medium or low", f.Severity)
	}
	if f.Owner && len(f.Edits) > 0 {
		return errors.New("an owner finding carries no edits: only the user can settle it")
	}
	if f.Axis == "evidence" && len(f.Sources) == 0 {
		return errors.New("an evidence finding names its sources")
	}
	for _, id := range f.Basis {
		if _, err := ParseID(id); err != nil {
			return fmt.Errorf("basis %q: %w", id, err)
		}
	}
	for i, edit := range f.Edits {
		if edit.Old == "" {
			return fmt.Errorf("edit %d: old must be the exact text to replace", i+1)
		}
	}
	return nil
}

// DeepAnswer is the drafter's answer to one finding.
type DeepAnswer struct {
	Target  string `json:"target"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
}

var deepFindingTarget = regexp.MustCompile(`^f[1-9][0-9]*$`)

// Validate checks the answer names a finding, accepts or contests it, and
// gives the reason of a contest.
func (a DeepAnswer) Validate() error {
	if !deepFindingTarget.MatchString(a.Target) {
		return fmt.Errorf("target %q is not a finding number such as f1", a.Target)
	}
	switch a.Verdict {
	case "accept":
	case "contest":
		if strings.TrimSpace(a.Reason) == "" {
			return fmt.Errorf("contesting %s needs the reason in words", a.Target)
		}
	default:
		return fmt.Errorf("verdict %q is not accept or contest", a.Verdict)
	}
	return nil
}

// Deep response outcomes: how the verifier answers a contested finding.
const (
	DeepHeld     = "held"
	DeepConceded = "conceded"
	DeepRevised  = "revised"
	DeepDropped  = "dropped"
	// DeepApplied is the outcome of a finding the drafter accepted.
	DeepApplied = "applied"
)

var deepOutcomes = []string{DeepHeld, DeepConceded, DeepRevised, DeepDropped}

// DeepResponse is the verifier's answer to one contested finding.
type DeepResponse struct {
	Target  string     `json:"target"`
	Outcome string     `json:"outcome"`
	Note    string     `json:"note,omitempty"`
	Edits   []DeepEdit `json:"edits,omitempty"`
}

// Validate checks the response names a finding, uses a known outcome, and
// explains every outcome but a revision.
func (r DeepResponse) Validate() error {
	if !deepFindingTarget.MatchString(r.Target) {
		return fmt.Errorf("target %q is not a finding number such as f1", r.Target)
	}
	if !slices.Contains(deepOutcomes, r.Outcome) {
		return fmt.Errorf("outcome %q is not one of %s", r.Outcome, strings.Join(deepOutcomes, ", "))
	}
	if r.Outcome != DeepRevised && strings.TrimSpace(r.Note) == "" {
		return fmt.Errorf("a %s response explains itself in a note", r.Outcome)
	}
	if r.Outcome == DeepDropped && len(r.Edits) > 0 {
		return errors.New("a dropped finding carries no edits")
	}
	for i, edit := range r.Edits {
		if edit.Old == "" {
			return fmt.Errorf("edit %d: old must be the exact text to replace", i+1)
		}
	}
	return nil
}

// DeepFindingID is the number sdd gives the finding at index i.
func DeepFindingID(i int) string { return "f" + strconv.Itoa(i+1) }

// DeepOpenFindings counts the findings the verifier still stands by: every
// finding whose latest response neither concedes nor drops it.
func DeepOpenFindings(findings []DeepFinding, responses []DeepResponse) int {
	open := 0
	for i := range findings {
		outcome := ""
		for _, response := range responses {
			if response.Target == DeepFindingID(i) {
				outcome = response.Outcome
			}
		}
		if outcome != DeepConceded && outcome != DeepDropped {
			open++
		}
	}
	return open
}

// DeepChange is the edits one finding carries into the proposal.
type DeepChange struct {
	Finding string
	Outcome string
	Edits   []DeepEdit
}

// DeepHunk is one applied edit of the proposal, labelled by its finding. Old
// and New are the whole lines the edit touches, before and after.
type DeepHunk struct {
	ID      string
	Finding string
	Outcome string
	Line    int
	Old     string
	New     string
}

// DeepChanges derives the proposal's changes: every finding but an owner one
// carries its own edits, and the verifier's latest response to a contested
// finding replaces them (held keeps them unless it names new ones, dropped
// clears them).
func DeepChanges(findings []DeepFinding, responses []DeepResponse) []DeepChange {
	var changes []DeepChange
	for i, finding := range findings {
		if finding.Owner {
			continue
		}
		id := DeepFindingID(i)
		change := DeepChange{Finding: id, Outcome: DeepApplied, Edits: finding.Edits}
		for _, response := range responses {
			if response.Target != id {
				continue
			}
			change.Outcome = response.Outcome
			if response.Outcome != DeepHeld || len(response.Edits) > 0 {
				change.Edits = response.Edits
			}
		}
		if len(change.Edits) > 0 {
			changes = append(changes, change)
		}
	}
	return changes
}

type deepSpan struct {
	start, end int
	finding    string
	outcome    string
	edit       DeepEdit
}

// BuildDeepProposal applies the changes to base: every old text must match
// exactly once and no two edits may overlap. It returns the proposal and its
// hunks in document order, or every problem found.
func BuildDeepProposal(base string, changes []DeepChange) (string, []DeepHunk, error) {
	var spans []deepSpan
	var problems []error
	for _, change := range changes {
		for i, edit := range change.Edits {
			count := strings.Count(base, edit.Old)
			if count != 1 {
				problems = append(problems, fmt.Errorf("%s edit %d: old text matches the draft %d times, it must match exactly once: %q", change.Finding, i+1, count, edit.Old))
				continue
			}
			start := strings.Index(base, edit.Old)
			spans = append(spans, deepSpan{start: start, end: start + len(edit.Old), finding: change.Finding, outcome: change.Outcome, edit: edit})
		}
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			problems = append(problems, fmt.Errorf("%s and %s edit overlapping text: %q", spans[i-1].finding, spans[i].finding, base[spans[i].start:min(spans[i].end, spans[i-1].end)]))
		}
	}
	if len(problems) > 0 {
		return "", nil, errors.Join(problems...)
	}
	var b strings.Builder
	hunks := make([]DeepHunk, 0, len(spans))
	last := 0
	for i, span := range spans {
		b.WriteString(base[last:span.start])
		b.WriteString(span.edit.New)
		last = span.end
		lineStart := strings.LastIndex(base[:span.start], "\n") + 1
		lineEnd := span.end + strings.IndexByte(base[span.end:]+"\n", '\n')
		hunks = append(hunks, DeepHunk{
			ID: "h" + strconv.Itoa(i+1), Finding: span.finding, Outcome: span.outcome,
			Line: strings.Count(base[:span.start], "\n") + 1, Old: base[lineStart:lineEnd], New: base[lineStart:span.start] + span.edit.New + base[span.end:lineEnd],
		})
	}
	b.WriteString(base[last:])
	return b.String(), hunks, nil
}

// RenderDeepHunks renders the hunks as a diff labelled by finding:
// `@@ h1 [f1 applied] line 12`, then the old lines and the new ones.
func RenderDeepHunks(hunks []DeepHunk) string {
	var b strings.Builder
	for _, hunk := range hunks {
		fmt.Fprintf(&b, "@@ %s [%s %s] line %d\n", hunk.ID, hunk.Finding, hunk.Outcome, hunk.Line)
		for _, line := range strings.Split(hunk.Old, "\n") {
			b.WriteString("-" + line + "\n")
		}
		if hunk.New != "" {
			for _, line := range strings.Split(hunk.New, "\n") {
				b.WriteString("+" + line + "\n")
			}
		}
	}
	return b.String()
}

// CheckDeepFindings lists what refuses a set of findings over base: an
// invalid finding, a basis or ref pointer that does not resolve, or edits
// that do not apply. Empty means the findings stand.
func CheckDeepFindings(base string, findings []DeepFinding, resolves func(id string) bool) []string {
	var problems []string
	for i, finding := range findings {
		id := DeepFindingID(i)
		if err := finding.Validate(); err != nil {
			problems = append(problems, id+": "+err.Error())
			continue
		}
		for _, basis := range finding.Basis {
			if !resolves(basis) {
				problems = append(problems, fmt.Sprintf("%s: basis %s resolves to no entry", id, basis))
			}
		}
		for _, pointer := range finding.Pointers {
			if pointer.ID != "" && !resolves(pointer.ID) {
				problems = append(problems, fmt.Sprintf("%s: pointer %s resolves to no entry", id, pointer.ID))
			}
		}
	}
	if len(problems) > 0 {
		return problems
	}
	_, _, err := BuildDeepProposal(base, DeepChanges(findings, nil))
	if err != nil {
		problems = append(problems, strings.Split(err.Error(), "\n")...)
	}
	return problems
}

// DeepWeighing is the fixed rule's recommendation on running the deep
// exchange (V-D24), with the reasons that made it.
type DeepWeighing struct {
	Run     bool
	Reasons []string
}

// DeepDraft is what the weighing rule reads of a draft.
type DeepDraft struct {
	Text       string
	Kind       Kind
	Layer      Layer
	Supersedes []string
	Closes     []string
	// MediumFindings counts the medium findings pre-flight left.
	MediumFindings int
}

var (
	deepURL        = regexp.MustCompile(`https?://[^\s)>\]]+`)
	deepCommitHash = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	deepCodeSpan   = regexp.MustCompile("`([^`\n]+)`")
	deepPathLike   = regexp.MustCompile(`^[\w.-]+(/[\w.-]+)+$|^[\w-]+\.[a-z]{1,5}$`)
)

// WeighDeep recommends running the deep exchange when the draft makes claims
// a tool can check (a path found in the checkout, a URL, a commit hash), is a
// directive, plan or contract on the strategic, conceptual or tactical
// layer, supersedes or closes an entry, or pre-flight left medium findings;
// otherwise it recommends skipping. found reports whether a path exists in
// the checkout.
func WeighDeep(draft DeepDraft, found func(path string) bool) DeepWeighing {
	var reasons []string
	for _, span := range deepCodeSpan.FindAllStringSubmatch(draft.Text, -1) {
		path := strings.TrimSpace(span[1])
		if deepPathLike.MatchString(path) && found(path) {
			reasons = append(reasons, fmt.Sprintf("it names %s, a path in the checkout", path))
			break
		}
	}
	if url := deepURL.FindString(draft.Text); url != "" {
		reasons = append(reasons, "it cites "+url)
	}
	for _, hash := range deepCommitHash.FindAllString(draft.Text, -1) {
		if strings.ContainsAny(hash, "abcdef") && strings.ContainsAny(hash, "0123456789") {
			reasons = append(reasons, "it names commit "+hash)
			break
		}
	}
	kind := draft.Kind
	if kind == "" {
		kind = KindDirective
	}
	if (kind == KindDirective || kind == KindPlan || kind == KindContract) && (draft.Layer == LayerStrategic || draft.Layer == LayerConceptual || draft.Layer == LayerTactical) {
		reasons = append(reasons, fmt.Sprintf("it is a %s on the %s layer", kind, draft.Layer))
	}
	if len(draft.Supersedes) > 0 || len(draft.Closes) > 0 {
		reasons = append(reasons, "it supersedes or closes an entry")
	}
	if draft.MediumFindings > 0 {
		reasons = append(reasons, fmt.Sprintf("pre-flight left %d medium finding(s)", draft.MediumFindings))
	}
	if len(reasons) == 0 {
		return DeepWeighing{Reasons: []string{"it makes no claim a tool can check, is no directive, plan or contract above the operational layer, retires nothing, and pre-flight left no medium finding"}}
	}
	return DeepWeighing{Run: true, Reasons: reasons}
}

// DeepReadingBlock is one entry's block of the deep verifier's reading.
type DeepReadingBlock struct {
	ID   string
	Text string
}

// DeepReadingPage is one page of the deep verifier's reading with the entries
// it serves in full, which the consumer holds once it is served (R14 of
// 20261109-004506-d-cpt-udz).
type DeepReadingPage struct {
	Text    string
	FullIDs []string
}

// DeepReadingPages pages the deep verifier's reading (V-D8): the first page
// opens with lead (the draft and what pre-flight raised) and the blocks of
// first (the declared targets and the entries read in full), later pages hold
// rest (the briefs) in order. A page holds whole blocks of at most
// DeepPageChars; a block larger than a page has a page of its own, never cut.
// The blocks of first are served in full, those of rest as briefs.
func DeepReadingPages(lead string, first, rest []DeepReadingBlock) []DeepReadingPage {
	pages := []DeepReadingPage{{Text: lead}}
	add := func(block DeepReadingBlock, full, newPage bool) {
		current := &pages[len(pages)-1]
		if newPage || (current.Text != "" && len(current.Text)+2+len(block.Text) > DeepPageChars) {
			pages = append(pages, DeepReadingPage{Text: block.Text})
			current = &pages[len(pages)-1]
		} else {
			if current.Text != "" {
				current.Text += "\n\n"
			}
			current.Text += block.Text
		}
		if full {
			current.FullIDs = append(current.FullIDs, block.ID)
		}
	}
	for _, block := range first {
		add(block, true, false)
	}
	for i, block := range rest {
		add(block, false, i == 0)
	}
	return pages
}

// DeepDraftDocument is a draft document the deep exchange edits, read back
// into its fields: one `name: <JSON>` line per reported field, then, after an
// empty line, the body.
type DeepDraftDocument struct {
	Fields  map[string]any
	Body    string
	HasBody bool
}

// ParseDeepDraftDocument reads a draft document back into its fields,
// refusing a header line that names no field, names one twice or holds no
// JSON value.
func ParseDeepDraftDocument(doc string) (DeepDraftDocument, error) {
	parsed := DeepDraftDocument{Fields: map[string]any{}}
	rest := doc
	for rest != "" {
		if body, ok := strings.CutPrefix(rest, "\n"); ok {
			parsed.Body, parsed.HasBody = body, true
			break
		}
		line, after, _ := strings.Cut(rest, "\n")
		rest = after
		name, raw, ok := strings.Cut(line, ": ")
		if !ok || name == "" {
			return DeepDraftDocument{}, fmt.Errorf("header line %q is not `field: value`", line)
		}
		if _, seen := parsed.Fields[name]; seen {
			return DeepDraftDocument{}, fmt.Errorf("header field %s appears twice", name)
		}
		var value any
		err := json.Unmarshal([]byte(raw), &value)
		if err != nil {
			return DeepDraftDocument{}, fmt.Errorf("header field %s: %w", name, err)
		}
		parsed.Fields[name] = value
	}
	return parsed, nil
}

// DeepContinuedPages pages text that may run past one page of the reading:
// the first page opens with intro, each later page with continued, and no
// page exceeds DeepPageChars. The text breaks at a paragraph where it can,
// else at a line, else between runes, so the pages joined without their
// openings are the text again.
func DeepContinuedPages(intro, continued, text string) []DeepReadingPage {
	room := DeepPageChars - max(len(intro), len(continued))
	var pages []DeepReadingPage
	opening := intro
	for {
		chunk := text
		if len(chunk) > room {
			chunk = text[:deepBreak(text, room)]
		}
		pages = append(pages, DeepReadingPage{Text: opening + chunk})
		text = text[len(chunk):]
		if text == "" {
			return pages
		}
		opening = continued
	}
}

// deepBreak is where text longer than room breaks: after the last paragraph
// or line break within room, else at the last rune start.
func deepBreak(text string, room int) int {
	head := text[:room]
	if i := strings.LastIndex(head, "\n\n"); i > 0 {
		return i + 2
	}
	if i := strings.LastIndexByte(head, '\n'); i > 0 {
		return i + 1
	}
	cut := room
	for cut > 1 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return cut
}

// The causes of a deep verifier skip, both the user's alone.
const (
	DeepSkipFailure = "failure"
	DeepSkipWeighed = "weighed"
)

// MarkVerificationSkipped marks e as written without the deep verifier's
// verdict and caps its confidence: low after a verifier failure (V-D4), at
// most medium after the weighed rule's skip (V-D24).
func MarkVerificationSkipped(e *Entry, cause string) error {
	switch cause {
	case DeepSkipFailure:
		e.Confidence = "low"
	case DeepSkipWeighed:
		if e.Confidence == "high" {
			e.Confidence = "medium"
		}
	default:
		return fmt.Errorf("verification skip %q is not failure or weighed", cause)
	}
	e.Verification = VerificationSkipped
	return nil
}

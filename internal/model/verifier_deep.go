package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The deep verifier exchange: the verifier returns findings with per-finding
// search-replace edits over the base draft document, the drafter accepts or
// contests each, the verifier answers the contested ones, and sdd builds the
// proposal, its hunks and the finding each hunk carries.

// DeepRounds is how many deliveries a deep verifier makes before the open
// points go to the user.
const DeepRounds = 3

// DeepRevisions is how many revisions of a draft one deep verifier judges
// before a fresh one starts; each opens DeepRounds deliveries of its own.
const DeepRevisions = 3

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

// The kinds of source a deep finding cites.
const (
	DeepSourceFile   = "file"
	DeepSourceCommit = "commit"
	DeepSourceURL    = "url"
	DeepSourceEntry  = "entry"
)

var deepSourceKinds = []string{DeepSourceFile, DeepSourceCommit, DeepSourceURL, DeepSourceEntry}

// DeepSourceKinds lists the closed set of kinds a deep source names.
func DeepSourceKinds() []string { return slices.Clone(deepSourceKinds) }

// DeepSource is one source a finding cites: a checkout file, a commit, a URL
// or a graph entry, with the words it quotes from it.
type DeepSource struct {
	Kind  string `json:"kind"`
	Ref   string `json:"ref"`
	Quote string `json:"quote,omitempty"`
}

var deepCommitRef = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// Validate checks the source's form: a known kind, a path inside the
// checkout, a commit hash, an http or https URL, or a full entry ID.
func (s DeepSource) Validate() error {
	switch s.Kind {
	case DeepSourceFile:
		if !fs.ValidPath(s.Ref) || s.Ref == "." {
			return fmt.Errorf("source file %q is no path inside the checkout, relative to its root", s.Ref)
		}
	case DeepSourceCommit:
		if !deepCommitRef.MatchString(s.Ref) {
			return fmt.Errorf("source commit %q is no commit hash", s.Ref)
		}
	case DeepSourceURL:
		parsed, err := url.Parse(s.Ref)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("source url %q is no http or https URL", s.Ref)
		}
	case DeepSourceEntry:
		if _, err := ParseID(s.Ref); err != nil {
			return fmt.Errorf("source entry %q: %w", s.Ref, err)
		}
	default:
		return fmt.Errorf("source kind %q is not one of %s", s.Kind, strings.Join(deepSourceKinds, ", "))
	}
	return nil
}

// DeepLookup resolves what deep findings cite. A nil function finds nothing.
// Each returns the text a source's quote must be in: an entry's summary and
// body, a file's content, a commit's message.
type DeepLookup struct {
	Entry func(id string) (text string, found bool)
	// File returns the content of a file in the checkout; found is false when
	// no file is at path.
	File   func(path string) (content string, found bool, err error)
	Commit func(hash string) (message string, found bool, err error)
}

// DeepFinding is one finding of the deep verifier, reasoning first.
type DeepFinding struct {
	Verdict  string        `json:"verdict"`
	Axis     string        `json:"axis"`
	Severity string        `json:"severity"`
	Owner    bool          `json:"owner,omitempty"`
	Pointers []DeepPointer `json:"pointers,omitempty"`
	Basis    []string      `json:"basis,omitempty"`
	Sources  []DeepSource  `json:"sources,omitempty"`
	Edits    []DeepEdit    `json:"edits,omitempty"`
}

// Validate checks the finding's shape: the closed axis and severity sets, an
// owner finding without edits, evidence with sources unless only the user can
// settle it, full entry IDs, and sources of a valid form.
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
	if f.Axis == "evidence" && !f.Owner && len(f.Sources) == 0 {
		return errors.New("an evidence finding names its sources")
	}
	for _, id := range f.Basis {
		if _, err := ParseID(id); err != nil {
			return fmt.Errorf("basis %q: %w", id, err)
		}
	}
	for _, source := range f.Sources {
		if err := source.Validate(); err != nil {
			return err
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

// DeepFindingLabel names a finding by its number, severity and axis, the way
// every listing after the one serving it in full refers to it.
func DeepFindingLabel(id string, finding DeepFinding) string {
	return fmt.Sprintf("%s [%s, %s]", id, finding.Severity, finding.Axis)
}

// DeepSettledLine renders a settled finding as one line: its label, how it
// was settled and the note, if any, with its line breaks folded.
func DeepSettledLine(id string, finding DeepFinding, settled, note string) string {
	line := "- " + DeepFindingLabel(id, finding) + " " + settled
	if note = strings.Join(strings.Fields(note), " "); note != "" {
		line += ": " + note
	}
	return line + "\n"
}

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
// and New are the changed span before and after, with up to
// DeepHunkContextWords words of its lines around it; an ellipsis marks a line
// that goes on.
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

// DeepEditsReverted reports whether draft is document with one or more of
// the edits the proposal wrote over base taken back at the spot it wrote
// them, and nothing else changed: the user undid what the verifier wrote,
// which needs no new check. document must be the proposal itself.
func DeepEditsReverted(base, document, draft string, changes []DeepChange) bool {
	spans, problems := deepEditSpans(base, changes)
	if len(problems) > 0 || len(spans) == 0 || draft == document {
		return false
	}
	type part struct{ fixed, wrote, took string }
	var parts []part
	var proposal strings.Builder
	last := 0
	for _, span := range spans {
		parts = append(parts, part{fixed: base[last:span.start]}, part{wrote: span.edit.New, took: span.edit.Old})
		proposal.WriteString(base[last:span.start])
		proposal.WriteString(span.edit.New)
		last = span.end
	}
	parts = append(parts, part{fixed: base[last:]})
	proposal.WriteString(base[last:])
	if proposal.String() != document {
		return false
	}
	seen := map[[2]int]bool{}
	var match func(i, at int) bool
	match = func(i, at int) bool {
		if i == len(parts) {
			return at == len(draft)
		}
		key := [2]int{i, at}
		if seen[key] {
			return false
		}
		seen[key] = true
		p := parts[i]
		if p.wrote == "" && p.took == "" {
			return strings.HasPrefix(draft[at:], p.fixed) && match(i+1, at+len(p.fixed))
		}
		for _, text := range []string{p.wrote, p.took} {
			if strings.HasPrefix(draft[at:], text) && match(i+1, at+len(text)) {
				return true
			}
		}
		return false
	}
	return match(0, 0)
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
	spans, problems := deepEditSpans(base, changes)
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
		before, after := deepHunkContext(base, span.start, span.end)
		hunks = append(hunks, DeepHunk{
			ID: "h" + strconv.Itoa(i+1), Finding: span.finding, Outcome: span.outcome,
			Line: strings.Count(base[:span.start], "\n") + 1, Old: before + span.edit.Old + after, New: before + span.edit.New + after,
		})
	}
	b.WriteString(base[last:])
	return b.String(), hunks, nil
}

// DeepHunkContextWords is how many words of its lines a hunk carries on each
// side of the changed span.
const DeepHunkContextWords = 3

// deepHunkContext is the text of base a hunk carries around the span
// start..end: up to DeepHunkContextWords words on each side, never past the
// span's lines, with an ellipsis where a line goes on.
func deepHunkContext(base string, start, end int) (string, string) {
	lineStart := strings.LastIndex(base[:start], "\n") + 1
	lineEnd := end + strings.IndexByte(base[end:]+"\n", '\n')
	from := start
	for words := 0; words < DeepHunkContextWords && from > lineStart; words++ {
		for from > lineStart && base[from-1] == ' ' {
			from--
		}
		for from > lineStart && base[from-1] != ' ' {
			from--
		}
	}
	to := end
	for words := 0; words < DeepHunkContextWords && to < lineEnd; words++ {
		for to < lineEnd && base[to] == ' ' {
			to++
		}
		for to < lineEnd && base[to] != ' ' {
			to++
		}
	}
	before, after := base[from:start], base[end:to]
	if from > lineStart {
		before = "…" + before
	}
	if to < lineEnd {
		after += "…"
	}
	return before, after
}

// deepEditSpans places every edit over base, sorted by position: each old
// text must match exactly once and no two may overlap.
func deepEditSpans(base string, changes []DeepChange) ([]deepSpan, []error) {
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
	return spans, problems
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
// invalid finding, a basis, ref pointer or cited source that does not
// resolve, a quote its draft or file does not hold, or edits that do not
// apply. Empty means the findings stand; an error is a lookup that failed.
func CheckDeepFindings(base string, findings []DeepFinding, lookup DeepLookup) ([]string, error) {
	resolves := func(id string) bool {
		if lookup.Entry == nil {
			return false
		}
		_, found := lookup.Entry(id)
		return found
	}
	draft := newDeepDraftTexts(base)
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
			if pointer.Quote != "" && !draft.holds(pointer.Quote) {
				problems = append(problems, fmt.Sprintf("%s: pointer quote %q is not in the draft", id, pointer.Quote))
			}
		}
		for _, source := range finding.Sources {
			problem, err := checkDeepSource(source, lookup)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", id, err)
			}
			if problem != "" {
				problems = append(problems, id+": "+problem)
			}
		}
	}
	if len(problems) > 0 {
		return problems, nil
	}
	_, _, err := BuildDeepProposal(base, DeepChanges(findings, nil))
	if err != nil {
		problems = append(problems, strings.Split(err.Error(), "\n")...)
	}
	return problems, nil
}

// checkDeepSource names why a valid source does not resolve, empty when it
// does: its file, commit or entry is missing, or does not hold its quote.
// Quotes match with whitespace runs collapsed.
func checkDeepSource(source DeepSource, lookup DeepLookup) (string, error) {
	var (
		text  string
		found bool
		err   error
		what  string
	)
	switch source.Kind {
	case DeepSourceFile:
		what = fmt.Sprintf("source file %s", source.Ref)
		if lookup.File != nil {
			text, found, err = lookup.File(source.Ref)
		}
		if err == nil && !found {
			return what + " is not in the checkout", nil
		}
	case DeepSourceCommit:
		what = fmt.Sprintf("source commit %s", source.Ref)
		if lookup.Commit != nil {
			text, found, err = lookup.Commit(source.Ref)
		}
		if err == nil && !found {
			return what + " is not in the repository", nil
		}
	case DeepSourceEntry:
		what = fmt.Sprintf("source entry %s", source.Ref)
		if lookup.Entry != nil {
			text, found = lookup.Entry(source.Ref)
		}
		if !found {
			return what + " resolves to no entry", nil
		}
	default:
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if source.Quote != "" && !strings.Contains(collapseSpace(text), collapseSpace(source.Quote)) {
		return fmt.Sprintf("%s does not hold the quote %q", what, source.Quote), nil
	}
	return "", nil
}

// deepDraftTexts is the draft document a pointer quotes, whitespace
// collapsed: the document as written and each header field's value decoded.
type deepDraftTexts []string

func newDeepDraftTexts(base string) deepDraftTexts {
	texts := deepDraftTexts{collapseSpace(base)}
	parsed, err := ParseDeepDraftDocument(base)
	if err != nil {
		return texts
	}
	var add func(value any)
	add = func(value any) {
		switch v := value.(type) {
		case string:
			texts = append(texts, collapseSpace(v))
		case []any:
			for _, item := range v {
				add(item)
			}
		case map[string]any:
			for _, item := range v {
				add(item)
			}
		}
	}
	for _, value := range parsed.Fields {
		add(value)
	}
	return texts
}

func (d deepDraftTexts) holds(quote string) bool {
	quote = collapseSpace(quote)
	return slices.ContainsFunc(d, func(text string) bool { return strings.Contains(text, quote) })
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// DeepWeighing is the fixed rule's recommendation on running the deep
// exchange, with the reasons that made it.
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
	ID   string `json:"id"`
	Text string `json:"text"`
}

// DeepReadingPage is one page of the deep verifier's reading with the entries
// it serves in full, which the consumer holds once it is served.
type DeepReadingPage struct {
	Text    string
	FullIDs []string
}

// DeepContinued is text that may run past one page of the reading: its first
// page opens with Intro, each later page with Continued.
type DeepContinued struct {
	Intro     string `json:"intro,omitempty"`
	Continued string `json:"continued,omitempty"`
	Text      string `json:"text,omitempty"`
}

// DeepReading is the deep verifier's whole reading, kept whole so that each
// page is cut for the host it is served to (20261122-103723-d-tac-npg): Lead
// (the draft and what pre-flight raised), Rules, and the blocks of First (the
// declared targets and the entries read in full), then those of Rest (the
// briefs).
type DeepReading struct {
	Lead  string             `json:"lead"`
	Rules DeepContinued      `json:"rules"`
	First []DeepReadingBlock `json:"first,omitempty"`
	Rest  []DeepReadingBlock `json:"rest,omitempty"`
}

// Pages pages the reading for a result within bound (DeepReadingPages).
func (r DeepReading) Pages(bound ResultBound) []DeepReadingPage {
	var rules []DeepReadingPage
	if r.Rules.Text != "" {
		rules = DeepContinuedPages(r.Rules.Intro, r.Rules.Continued, r.Rules.Text, bound)
	}
	return DeepReadingPages(r.Lead, rules, r.First, r.Rest, bound)
}

// DeepReadingPages pages the deep verifier's reading: the first page
// opens with lead (the draft and what pre-flight raised), the pages of rules
// follow it, and the blocks of first (the declared targets and the entries
// read in full), then those of rest (the briefs), fill the room the pages
// before them leave, in order: page 1 takes blocks until one does not fit,
// the rules' last page takes the blocks after it. A page holds whole blocks
// of at most bound.DeepPage; a block larger than a page runs on across pages
// of its own, never cut, and its last page names it. The blocks of first are
// served in full, those of rest as briefs.
func DeepReadingPages(lead string, rules []DeepReadingPage, first, rest []DeepReadingBlock, bound ResultBound) []DeepReadingPage {
	pageChars := bound.DeepPage()
	pages := []DeepReadingPage{{Text: lead}}
	placeRules := func() {
		pages = append(pages, rules...)
		rules = nil
	}
	fits := func(block DeepReadingBlock) bool {
		current := pages[len(pages)-1].Text
		return current == "" || ServedChars(current+"\n\n"+block.Text) <= pageChars
	}
	add := func(block DeepReadingBlock, full bool) {
		if ServedChars(block.Text) > pageChars {
			placeRules()
			pages = append(pages, DeepContinuedPages("", "("+block.ID+", continued)\n\n", block.Text, bound)...)
		} else {
			if !fits(block) {
				placeRules()
				if !fits(block) {
					pages = append(pages, DeepReadingPage{})
				}
			}
			current := &pages[len(pages)-1]
			if current.Text != "" {
				current.Text += "\n\n"
			}
			current.Text += block.Text
		}
		if full {
			current := &pages[len(pages)-1]
			current.FullIDs = append(current.FullIDs, block.ID)
		}
	}
	for _, block := range first {
		add(block, true)
	}
	for _, block := range rest {
		add(block, false)
	}
	placeRules()
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
// page exceeds bound.DeepPage. The text breaks at a paragraph where it can,
// else at a line, else between runes, so the pages joined without their
// openings are the text again.
func DeepContinuedPages(intro, continued, text string, bound ResultBound) []DeepReadingPage {
	chunks := SplitServed(text, bound.DeepPage()-max(ServedChars(intro), ServedChars(continued)))
	pages := make([]DeepReadingPage, len(chunks))
	for i, chunk := range chunks {
		opening := continued
		if i == 0 {
			opening = intro
		}
		pages[i] = DeepReadingPage{Text: opening + chunk}
	}
	return pages
}

// The causes of a verifier skip, both the user's alone.
const (
	DeepSkipFailure = "failure"
	DeepSkipWeighed = "weighed"
)

// MarkVerificationSkipped marks e as written without a verifier's verdict and
// caps its confidence: low after a verifier failure or a pre-flight answer
// cut at the token cap, at most medium after the weighed rule's skip.
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

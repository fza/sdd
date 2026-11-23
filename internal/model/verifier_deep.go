package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
	// Rev names the revision a file source is read at; empty reads the
	// checkout as it stands.
	Rev   string `json:"rev,omitempty"`
	Quote string `json:"quote,omitempty"`
	// formless marks a string an earlier build stored that fits no kind's
	// form; that build checked none, so it is carried as a file unchecked.
	formless bool
}

var deepCommitRef = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// UnmarshalJSON also reads the plain string an earlier build stored as a
// source: the first of url, entry, commit and file whose form it fits.
func (s *DeepSource) UnmarshalJSON(data []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(`"`)) {
		var ref string
		err := json.Unmarshal(data, &ref)
		if err != nil {
			return err
		}
		*s = deepSourceOf(ref)
		return nil
	}
	type object DeepSource
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*object)(s))
}

func deepSourceOf(ref string) DeepSource {
	for _, kind := range []string{DeepSourceURL, DeepSourceEntry, DeepSourceCommit, DeepSourceFile} {
		source := DeepSource{Kind: kind, Ref: ref}
		if source.Validate() == nil {
			return source
		}
	}
	return DeepSource{Kind: DeepSourceFile, Ref: ref, formless: true}
}

// DeepSources is the list of sources a finding cites.
type DeepSources []DeepSource

// UnmarshalJSON also reads the plain string an earlier session log can hold
// in place of the list, as a one-item list.
func (s *DeepSources) UnmarshalJSON(data []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(`"`)) {
		var one DeepSource
		err := json.Unmarshal(data, &one)
		if err != nil {
			return err
		}
		*s = DeepSources{one}
		return nil
	}
	return json.Unmarshal(data, (*[]DeepSource)(s))
}

// Validate checks the source's form: a known kind, a path inside the
// checkout, a commit hash, an http or https URL, or a full entry ID.
func (s DeepSource) Validate() error {
	if s.formless {
		return nil
	}
	if s.Rev != "" && s.Kind != DeepSourceFile {
		return fmt.Errorf("source %s %q: only a file source names a rev", s.Kind, s.Ref)
	}
	switch s.Kind {
	case DeepSourceFile:
		if !GitPathInCheckout(s.Ref) {
			return fmt.Errorf("source file %q is no path inside the checkout, relative to its root", s.Ref)
		}
		if s.Rev != "" {
			err := ValidGitRevision(s.Rev)
			if err != nil {
				return fmt.Errorf("source file %q: %w", s.Ref, err)
			}
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
	File func(path string) (content string, found bool, err error)
	// FileAt returns the content of a file at a revision of the checkout's
	// repository; found is false when the revision or the file is missing.
	FileAt func(rev, path string) (content string, found bool, err error)
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
	Sources  DeepSources   `json:"sources,omitempty"`
	Edits    []DeepEdit    `json:"edits,omitempty"`
}

// Validate checks the finding's shape: the closed axis and severity sets, an
// owner finding without edits, evidence with sources unless only the user can
// settle it or it asks the drafter for a source, full entry IDs, and sources
// of a valid form.
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
	if f.Axis == "evidence" && !f.Owner && len(f.Sources) == 0 && !f.asksForSource() {
		return errors.New("an evidence finding names its sources; one that asks the drafter for a source carries no edits and a pointer quoting the claim; one only the user can settle sets owner: true")
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

// asksForSource reports whether the finding asks the drafter for a source of
// a claim it quotes, as an evidence finding without sources may
// (20261123-114150-d-tac-d8f): it carries no edits and a pointer quoting the
// claim, which CheckDeepFindings finds in the draft.
func (f DeepFinding) asksForSource() bool {
	return len(f.Edits) == 0 && slices.ContainsFunc(f.Pointers, func(p DeepPointer) bool { return strings.TrimSpace(p.Quote) != "" })
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

// DeepEditSettled reports whether draft differs from document only by
// reverting or restoring edits the proposal built from base and changes
// applied: both must be that proposal with each edit, at the place it was
// applied, holding its new text or its old one. Any other change is not
// settled, nor is any change when the edits no longer apply to base.
func DeepEditSettled(base string, changes []DeepChange, document, draft string) bool {
	spans, err := deepEditSpans(base, changes)
	if err != nil {
		return false
	}
	return deepEditVariant(base, spans, document) && deepEditVariant(base, spans, draft)
}

// deepEditVariant reports whether text is base with each span's edit
// applied or not.
func deepEditVariant(base string, spans []deepSpan, text string) bool {
	failed := map[[2]int]bool{}
	var match func(i, at int) bool
	match = func(i, at int) bool {
		from, to := 0, len(base)
		if i > 0 {
			from = spans[i-1].end
		}
		if i < len(spans) {
			to = spans[i].start
		}
		if !strings.HasPrefix(text[at:], base[from:to]) {
			return false
		}
		at += to - from
		if i == len(spans) {
			return at == len(text)
		}
		if failed[[2]int{i, at}] {
			return false
		}
		for _, choice := range []string{spans[i].edit.New, spans[i].edit.Old} {
			if strings.HasPrefix(text[at:], choice) && match(i+1, at+len(choice)) {
				return true
			}
		}
		failed[[2]int{i, at}] = true
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
	spans, err := deepEditSpans(base, changes)
	if err != nil {
		return "", nil, err
	}
	hunks := make([]DeepHunk, 0, len(spans))
	for i, span := range spans {
		before, after := deepHunkContext(base, span.start, span.end)
		hunks = append(hunks, DeepHunk{
			ID: "h" + strconv.Itoa(i+1), Finding: span.finding, Outcome: span.outcome,
			Line: strings.Count(base[:span.start], "\n") + 1, Old: before + span.edit.Old + after, New: before + span.edit.New + after,
		})
	}
	proposal, _ := applyDeepSpans(base, spans)
	return proposal, hunks, nil
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

// deepWritten is the text one edit wrote into the proposal, by offset.
type deepWritten struct {
	start, end int
	finding    string
}

// applyDeepSpans returns base with the spans' edits applied, and where each
// edit's new text stands in the result.
func applyDeepSpans(base string, spans []deepSpan) (string, []deepWritten) {
	var b strings.Builder
	written := make([]deepWritten, 0, len(spans))
	last := 0
	for _, span := range spans {
		b.WriteString(base[last:span.start])
		written = append(written, deepWritten{start: b.Len(), end: b.Len() + len(span.edit.New), finding: span.finding})
		b.WriteString(span.edit.New)
		last = span.end
	}
	b.WriteString(base[last:])
	return b.String(), written
}

// MergeDeepBody applies the drafter's edits to the body of the proposal base
// and changes build. Each edit's old text must match that body exactly once
// as it stands when the edit applies, and must not overlap text a change
// wrote.
func MergeDeepBody(base string, changes []DeepChange, edits []DeepEdit) (string, error) {
	spans, err := deepEditSpans(base, changes)
	if err != nil {
		return "", err
	}
	proposal, written := applyDeepSpans(base, spans)
	parsed, err := ParseDeepDraftDocument(proposal)
	if err != nil {
		return "", err
	}
	if !parsed.HasBody {
		return "", errors.New("the proposal holds no body")
	}
	body := parsed.Body
	offset := len(proposal) - len(body)
	for i := range written {
		written[i].start -= offset
		written[i].end -= offset
	}
	for i, edit := range edits {
		if edit.Old == "" {
			return "", fmt.Errorf("pair %d: old text is empty", i+1)
		}
		switch n := strings.Count(body, edit.Old); n {
		case 1:
		case 0:
			return "", fmt.Errorf("pair %d: old text is not in the proposal's body, so a hunk may have rewritten it: %q", i+1, edit.Old)
		default:
			return "", fmt.Errorf("pair %d: old text matches the proposal's body %d times, it must match exactly once: %q", i+1, n, edit.Old)
		}
		start := strings.Index(body, edit.Old)
		end := start + len(edit.Old)
		for _, w := range written {
			if start < w.end && w.start < end {
				return "", fmt.Errorf("pair %d edits text %s's edit changes: %q", i+1, w.finding, edit.Old)
			}
		}
		body = body[:start] + edit.New + body[end:]
		for j := range written {
			if written[j].start >= end {
				written[j].start += len(edit.New) - len(edit.Old)
				written[j].end += len(edit.New) - len(edit.Old)
			}
		}
	}
	return body, nil
}

// deepEditSpans places each edit of the changes in base, in document order,
// or returns every problem found.
func deepEditSpans(base string, changes []DeepChange) ([]deepSpan, error) {
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
		return nil, errors.Join(problems...)
	}
	return spans, nil
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
	switch source.Kind {
	case DeepSourceFile:
		where, at := "the checkout", ""
		read := lookup.File
		if source.Rev != "" {
			where, at = "revision "+source.Rev, " at revision "+source.Rev
			read = nil
			if lookup.FileAt != nil {
				read = func(path string) (string, bool, error) { return lookup.FileAt(source.Rev, path) }
			}
		}
		if read == nil {
			return fmt.Sprintf("source file %s is not in %s", source.Ref, where), nil
		}
		content, found, err := read(source.Ref)
		if err != nil {
			return "", err
		}
		if !found {
			return fmt.Sprintf("source file %s is not in %s", source.Ref, where), nil
		}
		if source.Quote != "" && !strings.Contains(collapseSpace(content), collapseSpace(source.Quote)) {
			return fmt.Sprintf("source file %s%s does not hold the quote %q", source.Ref, at, source.Quote), nil
		}
	case DeepSourceCommit:
		message, found := "", false
		if lookup.Commit != nil {
			var err error
			message, found, err = lookup.Commit(source.Ref)
			if err != nil {
				return "", err
			}
		}
		if !found {
			return fmt.Sprintf("source commit %s is not in the repository", source.Ref), nil
		}
		if source.Quote != "" && !strings.Contains(collapseSpace(message), collapseSpace(source.Quote)) {
			return fmt.Sprintf("source commit %s does not hold the quote %q", source.Ref, source.Quote), nil
		}
	case DeepSourceEntry:
		text, found := "", false
		if lookup.Entry != nil {
			text, found = lookup.Entry(source.Ref)
		}
		if !found {
			return fmt.Sprintf("source entry %s resolves to no entry", source.Ref), nil
		}
		if source.Quote != "" && !strings.Contains(collapseSpace(text), collapseSpace(source.Quote)) {
			return fmt.Sprintf("source entry %s does not hold the quote %q", source.Ref, source.Quote), nil
		}
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
// page is cut for the host it is served to: Lead (the draft, its refs' fit
// and what pre-flight raised), Rules, and the blocks of First (the declared
// targets and the entries read in full), then those of Rest (the listed
// lines).
type DeepReading struct {
	Lead  string             `json:"lead"`
	Rules DeepContinued      `json:"rules"`
	First []DeepReadingBlock `json:"first,omitempty"`
	Rest  []DeepReadingBlock `json:"rest,omitempty"`
}

// Pages pages the reading for a result within bound: the first page opens
// with the lead, the pages of rules follow it, and the blocks of first, then
// those of rest, fill the room the pages before them leave, in order: the
// lead's last page takes blocks until one does not fit, the rules' last page
// takes the blocks after it. A page holds whole blocks of at most
// bound.DeepPage, the first page of at most bound.DeepFirstPage; a lead or
// block larger than a page runs on across pages of its own, never cut, and a
// block's last page names it. The blocks of first are served in full, those
// of rest as lines.
func (r DeepReading) Pages(bound ResultBound) []DeepReadingPage {
	pages := []DeepReadingPage{{Text: r.Lead}}
	if ServedChars(r.Lead) > bound.DeepFirstPage() {
		pages = continuedPages("", "(continued from the page before)\n\n", r.Lead, bound.DeepFirstPage(), bound)
	}
	var rules []DeepReadingPage
	if r.Rules.Text != "" {
		rules = DeepContinuedPages(r.Rules.Intro, r.Rules.Continued, r.Rules.Text, bound)
	}
	placeRules := func() {
		pages = append(pages, rules...)
		rules = nil
	}
	fits := func(block DeepReadingBlock) bool {
		room := bound.DeepPage()
		if len(pages) == 1 {
			room = bound.DeepFirstPage()
		}
		text := block.Text
		if current := pages[len(pages)-1].Text; current != "" {
			text = current + "\n\n" + text
		}
		return ServedChars(text) <= room
	}
	add := func(block DeepReadingBlock, full bool) {
		if ServedChars(block.Text) > bound.DeepPage() {
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
	for _, block := range r.First {
		add(block, true)
	}
	for _, block := range r.Rest {
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
// else at a line, else between runes. A break inside a ``` fence closes the
// fence on its page and reopens it after the next page's opening, so the
// pages joined without their openings and those added fence lines are the
// text again.
func DeepContinuedPages(intro, continued, text string, bound ResultBound) []DeepReadingPage {
	return continuedPages(intro, continued, text, bound.DeepPage(), bound)
}

// continuedPages is DeepContinuedPages with its first page bounded by first.
func continuedPages(intro, continued, text string, first int, bound ResultBound) []DeepReadingPage {
	spare := max(ServedChars(intro), ServedChars(continued))
	if strings.Contains(text, deepFence) {
		spare += ServedChars("\n" + deepFence + "\n" + deepFence + "\n")
	}
	chunks := SplitServed(text, first-spare)[:1]
	if after := text[len(chunks[0]):]; after != "" {
		chunks = append(chunks, SplitServed(after, bound.DeepPage()-spare)...)
	}
	pages := make([]DeepReadingPage, len(chunks))
	open, read := false, 0
	for i, chunk := range chunks {
		opening := continued
		if i == 0 {
			opening = intro
		}
		if open {
			opening += deepFence + "\n"
		}
		read += len(chunk)
		open = fenceOpen(text[:read])
		closing := ""
		if open && i < len(chunks)-1 {
			if !strings.HasSuffix(chunk, "\n") {
				closing = "\n"
			}
			closing += deepFence + "\n"
		}
		pages[i] = DeepReadingPage{Text: opening + chunk + closing}
	}
	return pages
}

const deepFence = "```"

// fenceOpen reports whether text ends inside a fence: an odd count of its
// lines open with one.
func fenceOpen(text string) bool {
	open := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, deepFence) {
			open = !open
		}
	}
	return open
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

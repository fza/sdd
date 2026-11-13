package llmops_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/viewlayout"
	"github.com/networkteam/sdd/pkg/llm"
)

type lightFinding struct {
	Observation string   `json:"observation"`
	Basis       []string `json:"basis"`
	Category    string   `json:"category"`
	Severity    string   `json:"severity"`
}

// lightRunner records every light call's prompt and answers each with the
// findings answer returns for it.
type lightRunner struct {
	mu      sync.Mutex
	prompts []llm.Request
	answer  func(req llm.Request) []lightFinding
}

func (r *lightRunner) Run(_ context.Context, req llm.Request) (llm.Result, error) {
	r.mu.Lock()
	r.prompts = append(r.prompts, req)
	r.mu.Unlock()
	var findings []lightFinding
	if r.answer != nil {
		findings = r.answer(req)
	}
	if findings == nil {
		findings = []lightFinding{}
	}
	data, err := json.Marshal(map[string]any{"findings": findings})
	return llm.Result{Text: string(data), Identity: llm.Identity{Provider: "test", Model: "m"}}, err
}

func lightGraph(t *testing.T, extra ...*model.Entry) *model.Graph {
	t.Helper()
	facts, err := basefacts.Entries(viewlayout.Vocabulary{})
	if err != nil {
		t.Fatal(err)
	}
	return model.NewGraph(append(facts, extra...))
}

func readingEntry(id, summary, content string, topics ...string) *model.Entry {
	parts, err := model.ParseID(id)
	if err != nil {
		panic(err)
	}
	e := &model.Entry{ID: id, Type: model.TypeFromAbbrev[parts.TypeCode], Layer: model.LayerFromAbbrev[parts.LayerCode],
		Kind: model.KindGap, Summary: summary, Content: content, Time: parts.Time}
	for _, label := range topics {
		path, err := model.ParseTopicPath(label)
		if err != nil {
			panic(err)
		}
		e.Topics = append(e.Topics, path)
	}
	return e
}

var lightDraft = &model.Entry{Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
	Content: "The capture writer drops the second attachment when two share a name.", Time: time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)}

// The deep reading's header keeps every field of the light one the ID does
// not carry, puts ID, kind and status on one line, and names a role other
// than brief.
func TestDeepReadingHeader(t *testing.T) {
	target := readingEntry("20261101-090000-s-tac-tgt", "The writer keeps one attachment per name.", "Body.", "capture/attachments")
	entry := readingEntry("20261101-100000-s-tac-ent", "Attachments with one name collide.", "Body.", "capture/attachments")
	entry.Refs = []model.Ref{{ID: target.ID, Kind: "builds-on", Desc: "the writer it observes"}}
	done := readingEntry("20261101-110000-s-tac-dne", "The writer keeps them all.", "Body.")
	done.Kind = model.KindDone
	drifted := readingEntry("20261101-120000-s-tac-drf", "An entry whose time is not its ID's.", "Body.")
	drifted.Time = drifted.Time.Add(time.Minute)
	graph := lightGraph(t, target, entry, done, drifted)
	for _, tc := range []struct {
		name  string
		entry *model.Entry
		want  string
	}{
		{"its ID carries type, layer and time", done, "ID: 20261101-110000-s-tac-dne | kind: done | Status: terminal\nSummary: The writer keeps them all.\n"},
		{"its time is not its ID's", drifted, "ID: 20261101-120000-s-tac-drf | Type: signal, kind: gap, layer: tactical | Status: open | Time: 2026-11-01 12:01:00\nSummary: An entry whose time is not its ID's.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := llmops.DeepReadingHeader(graph, tc.entry, model.LightRoleBrief, "")
			if got != tc.want {
				t.Errorf("the first line should name type, layer and time only where the ID does not, and a done entry's status in one word, got:\n%s", got)
			}
		})
	}
	const fields = "ID: 20261101-100000-s-tac-ent | kind: gap | Status: open\n" +
		"Topics: capture/attachments\n" +
		"Ref: builds-on 20261101-090000-s-tac-tgt (the writer it observes)\n" +
		"Summary: Attachments with one name collide.\n"
	for _, tc := range []struct {
		role model.LightRole
		code string
		want string
	}{
		{role: model.LightRoleBrief, want: fields},
		{role: model.LightRoleFull, want: fields + "Role in the reading: read in full\n"},
		{role: "declared target", want: fields + "Role in the reading: declared target\n"},
		{role: model.LightRoleSetAside, code: model.VerdictCovered, want: fields + "Role in the reading: set aside (code: " + model.VerdictCovered + ")\n"},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			got := llmops.DeepReadingHeader(graph, entry, tc.role, tc.code)
			if got != tc.want {
				t.Errorf("the header should keep every field on its compact lines and name only a role other than brief, got:\n%s", got)
			}
		})
	}
}

// A closed brief beside its closer comes short, its brief kept; served in
// full, or with its closer outside the reading, it keeps its whole block.
func TestDeepReadingBlock(t *testing.T) {
	closed := readingEntry("20261101-090000-s-tac-cls", "The writer drops attachments.", "The closed gap's body.", "capture/attachments")
	closed.Title = "Attachments dropped"
	closer := readingEntry("20261101-100000-s-tac-dne", "The writer keeps them now.", "Body.", "capture/attachments")
	closer.Kind = model.KindDone
	closer.Closes = []string{closed.ID}
	graph := lightGraph(t, closed, closer)
	whole := llmops.DeepReadingHeader(graph, closed, model.LightRoleBrief, "")
	inReading := func(id string) bool { return id == closed.ID || id == closer.ID }
	elsewhere := func(id string) bool { return id == closed.ID }
	for _, tc := range []struct {
		name string
		role model.LightRole
		code string
		full bool
		held func(string) bool
		want string
	}{
		{name: "brief beside its closer", role: model.LightRoleBrief, held: inReading,
			want: "---\n\nID: " + closed.ID + " | kind: gap | Status: closed by " + closer.ID + "\n\nAttachments dropped"},
		{name: "set aside beside its closer", role: model.LightRoleSetAside, code: model.VerdictCovered, held: inReading,
			want: "---\n\nID: " + closed.ID + " | kind: gap | Status: closed by " + closer.ID + "\nRole in the reading: set aside (code: " + model.VerdictCovered + ")\n\nAttachments dropped"},
		{name: "read in full", role: model.LightRoleFull, full: true, held: inReading,
			want: "---\n\n" + llmops.DeepReadingHeader(graph, closed, model.LightRoleFull, "") + "\nThe closed gap's body."},
		{name: "closer outside the reading", role: model.LightRoleBrief, held: elsewhere,
			want: "---\n\n" + whole + "\nAttachments dropped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			block := llmops.DeepReadingBlock(graph, closed, tc.role, tc.code, tc.full, tc.held)
			if block.ID != closed.ID || block.Text != tc.want {
				t.Errorf("the block should come short only for a brief whose closer the reading holds, got:\n%s", block.Text)
			}
		})
	}
}

func TestPreflightLight_CarriesTheReadingWithHeadersAndAxes(t *testing.T) {
	full := readingEntry("20261101-100000-s-tac-ful", "Attachments with one name collide.", "The writer keeps only one attachment per name.", "capture/attachments")
	brief := readingEntry("20261101-110000-s-tac-brf", "Staging keeps every handle.", "Long body never served.", "capture/staging")
	aside := readingEntry("20261101-120000-s-tac-asd", "Summary drift on long bodies.", "Body.", "capture/summary")
	graph := lightGraph(t, full, brief, aside)
	runner := &lightRunner{}
	result, err := llmops.PreflightLight(t.Context(), runner, lightDraft, graph, "", llmops.LightReading{
		Full: []string{full.ID}, Listed: []string{brief.ID, aside.ID},
		SetAside: []llmops.LightSetAside{{IDs: []string{aside.ID}, Code: model.VerdictCovered}},
	}, 1_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.prompts) != 1 || result.Calls != 1 {
		t.Fatalf("a reading that fits takes one call, got %d", len(runner.prompts))
	}
	prompt := runner.prompts[0].UserPrompt
	for _, want := range []string{
		"`duplicates`", "`outdated`", "`strain`", "`dismissal`", "`basis`",
		"ID: " + full.ID, "Role in the reading: read in full", full.Content,
		"ID: " + brief.ID, "Role in the reading: brief", "Staging keeps every handle.",
		"Role in the reading: set aside (code: " + model.VerdictCovered + ")",
		"Status: open", "Topics: capture/attachments",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the light prompt should carry %q", want)
		}
	}
	if strings.Contains(prompt, "Long body never served.") {
		t.Error("a listed entry should travel as its brief, not its body")
	}
	if strings.Contains(prompt, "Other calls judge the rest") {
		t.Error("a single call should not be told it carries a part")
	}
}

func TestPreflightLight_SlicesKeepTheSamePrefixAndMerge(t *testing.T) {
	body := strings.Repeat("Unrelated filler words about other matters entirely. ", 40)
	var entries []*model.Entry
	var listed []string
	for i, id := range []string{"20261101-100000-s-tac-aaa", "20261101-100001-s-tac-bbb", "20261101-100002-s-tac-ccc"} {
		e := readingEntry(id, "Entry "+string(rune('A'+i)), body, "topic/t"+string(rune('a'+i)))
		entries = append(entries, e)
		listed = append(listed, id)
	}
	graph := lightGraph(t, entries...)
	probe := &lightRunner{}
	_, err := llmops.PreflightLight(t.Context(), probe, lightDraft, graph, "", llmops.LightReading{}, 1_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixed := len(probe.prompts[0].SystemPrompt) + len(probe.prompts[0].UserPrompt)
	runner := &lightRunner{answer: func(req llm.Request) []lightFinding {
		return []lightFinding{{Observation: "The draft names no owner.", Category: "missing-owner", Severity: "medium", Basis: []string{}}}
	}}
	result, err := llmops.PreflightLight(t.Context(), runner, lightDraft, graph, "", llmops.LightReading{Full: listed}, fixed+len(body)+600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Calls != 3 || len(runner.prompts) != 3 {
		t.Fatalf("three unrelated full reads one call wide each take three calls, got %d", result.Calls)
	}
	prefix := runner.prompts[0].UserPrompt[:strings.Index(runner.prompts[0].UserPrompt, "## Reading")]
	for _, req := range runner.prompts {
		if req.SystemPrompt != runner.prompts[0].SystemPrompt || !strings.HasPrefix(req.UserPrompt, prefix) {
			t.Fatal("every call should open with the same rules and draft, byte for byte")
		}
		if !strings.Contains(req.UserPrompt, "Other calls judge the rest") {
			t.Error("a call carrying part of the reading should say so")
		}
	}
	if len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Observation, "raised by prose verifier call 1, 2, 3 of 3") {
		t.Fatalf("one finding raised by every call should collapse into one naming the calls, got %+v", result.Findings)
	}
}

func TestPreflightLight_ExcerptsAnOversizedEntry(t *testing.T) {
	paragraphs := []string{
		strings.Repeat("Filler about lunch menus and parking spaces. ", 8),
		"The capture writer drops the second attachment when two share a name.",
		strings.Repeat("More filler about office plants and coffee. ", 8),
	}
	big := readingEntry("20261101-100000-s-tac-big", "Attachment names.", strings.Join(paragraphs, "\n\n"))
	graph := lightGraph(t, big)
	probe := &lightRunner{}
	_, err := llmops.PreflightLight(t.Context(), probe, lightDraft, graph, "", llmops.LightReading{}, 1_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixed := len(probe.prompts[0].SystemPrompt) + len(probe.prompts[0].UserPrompt)
	runner := &lightRunner{}
	result, err := llmops.PreflightLight(t.Context(), runner, lightDraft, graph, "", llmops.LightReading{Full: []string{big.ID}}, fixed+700, nil)
	if err != nil {
		t.Fatal(err)
	}
	prompt := runner.prompts[0].UserPrompt
	if len(result.Excerpted) != 1 || result.Excerpted[0] != big.ID {
		t.Fatalf("the verdict should name the entry judged on excerpts, got %v", result.Excerpted)
	}
	if !strings.Contains(prompt, paragraphs[1]) || strings.Contains(prompt, paragraphs[0]) || !strings.Contains(prompt, model.ExcerptGap(1)) || !strings.Contains(prompt, "ID: "+big.ID) {
		t.Fatalf("the excerpt should keep the paragraph on the draft's subject, mark the gaps and keep the header:\n%s", prompt[strings.Index(prompt, "## Reading"):])
	}
}

func TestPreflightLight_RefusesADraftLargerThanTheCall(t *testing.T) {
	_, err := llmops.PreflightLight(t.Context(), &lightRunner{}, lightDraft, lightGraph(t), "", llmops.LightReading{}, 1000, nil)
	if err == nil || !strings.Contains(err.Error(), "llm.context_tokens") || !strings.Contains(err.Error(), "verify.mode: deep") {
		t.Fatalf("a draft larger than one call should be refused naming a larger window or deep, got %v", err)
	}
}

func TestPreflightLight_DropsBasisIDsThePromptNeverCarried(t *testing.T) {
	served := readingEntry("20261101-100000-s-tac-srv", "Served.", "Served body.")
	graph := lightGraph(t, served)
	runner := &lightRunner{answer: func(llm.Request) []lightFinding {
		return []lightFinding{{Observation: "Duplicates the served entry.", Category: "duplicates", Severity: "high", Basis: []string{served.ID, "20200101-000000-s-tac-inv"}}}
	}}
	result, err := llmops.PreflightLight(t.Context(), runner, lightDraft, graph, "", llmops.LightReading{Full: []string{served.ID}}, 1_000_000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Findings[0].Basis; len(got) != 1 || got[0] != served.ID {
		t.Fatalf("only IDs the prompt carried should stay in the basis, got %v", got)
	}
}

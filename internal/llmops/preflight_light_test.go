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

func TestPreflightLight_CarriesTheReadingWithHeadersAndAxes(t *testing.T) {
	full := readingEntry("20261101-100000-s-tac-ful", "Attachments with one name collide.", "The writer keeps only one attachment per name.", "capture/attachments")
	brief := readingEntry("20261101-110000-s-tac-brf", "Staging keeps every handle.", "Long body never served.", "capture/staging")
	aside := readingEntry("20261101-120000-s-tac-asd", "Summary drift on long bodies.", "Body.", "capture/summary")
	graph := lightGraph(t, full, brief, aside)
	runner := &lightRunner{}
	result, err := llmops.PreflightLight(t.Context(), runner, lightDraft, graph, "", llmops.LightReading{
		Full: []string{full.ID}, Listed: []string{brief.ID, aside.ID},
		SetAside: []llmops.LightSetAside{{IDs: []string{aside.ID}, Code: model.VerdictCovered}},
	}, 1_000_000)
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
	if strings.Contains(prompt, "other calls judge the rest") {
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
	_, err := llmops.PreflightLight(t.Context(), probe, lightDraft, graph, "", llmops.LightReading{}, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	fixed := len(probe.prompts[0].SystemPrompt) + len(probe.prompts[0].UserPrompt)
	runner := &lightRunner{answer: func(req llm.Request) []lightFinding {
		return []lightFinding{{Observation: "The draft names no owner.", Category: "missing-owner", Severity: "medium", Basis: []string{}}}
	}}
	result, err := llmops.PreflightLight(t.Context(), runner, lightDraft, graph, "", llmops.LightReading{Full: listed}, fixed+len(body)+600)
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
		if !strings.Contains(req.UserPrompt, "other calls judge the rest") {
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
	_, err := llmops.PreflightLight(t.Context(), probe, lightDraft, graph, "", llmops.LightReading{}, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	fixed := len(probe.prompts[0].SystemPrompt) + len(probe.prompts[0].UserPrompt)
	runner := &lightRunner{}
	result, err := llmops.PreflightLight(t.Context(), runner, lightDraft, graph, "", llmops.LightReading{Full: []string{big.ID}}, fixed+700)
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
	_, err := llmops.PreflightLight(t.Context(), &lightRunner{}, lightDraft, lightGraph(t), "", llmops.LightReading{}, 1000)
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
	result, err := llmops.PreflightLight(t.Context(), runner, lightDraft, graph, "", llmops.LightReading{Full: []string{served.ID}}, 1_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Findings[0].Basis; len(got) != 1 || got[0] != served.ID {
		t.Fatalf("only IDs the prompt carried should stay in the basis, got %v", got)
	}
}

package llmops_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm"
)

func titledFixture() (*model.Entry, *model.Graph) {
	related := &model.Entry{
		ID: "20260101-100000-s-tac-rel", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Title: "daemon project removal", Summary: "The daemon project is slated for removal.", Content: "Remove it.",
	}
	entry := &model.Entry{
		ID: "20260102-100000-d-tac-ent", Type: model.TypeDecision, Kind: model.KindDirective, Layer: model.LayerTactical,
		Refs: []model.Ref{{ID: related.ID, Kind: model.RefKindAddresses, Desc: "the ref description"}}, Summary: "A stored summary that may have been corrected.",
		Content: "The daemon project goes.",
	}
	return entry, model.NewGraph([]*model.Entry{related, entry})
}

func untitledCopy(entry *model.Entry) *model.Entry {
	untitled := *entry
	untitled.Title = ""
	return &untitled
}

func unsummarizedCopy(entry *model.Entry) *model.Entry {
	unsummarized := *entry
	unsummarized.Summary = ""
	unsummarized.Title = ""
	return &unsummarized
}

func TestNewDescriptionChoosesTheCall(t *testing.T) {
	entry, graph := titledFixture()
	cases := []struct {
		name          string
		entry         *model.Entry
		regenerate    bool
		wantPurpose   llm.Purpose
		wantTitleOnly bool
	}{
		{name: "no summary", entry: unsummarizedCopy(entry), wantPurpose: llm.PurposeSummarize},
		{name: "summary without title", entry: untitledCopy(entry), wantPurpose: llm.PurposeTitle, wantTitleOnly: true},
		{name: "summary without title, regenerated", entry: untitledCopy(entry), regenerate: true, wantPurpose: llm.PurposeSummarize},
		{name: "titled and summarized, regenerated", entry: entry, regenerate: true, wantPurpose: llm.PurposeSummarize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			description, err := llmops.NewDescription(tc.entry, graph, "", tc.regenerate, nil)
			if err != nil {
				t.Fatalf("NewDescription: %v", err)
			}
			if description.Request.Purpose != tc.wantPurpose || description.TitleOnly() != tc.wantTitleOnly {
				t.Errorf("the entry should get the call its state needs, got purpose %q, title only %v", description.Request.Purpose, description.TitleOnly())
			}
		})
	}
}

func TestNewDescriptionRefusesADescribedEntry(t *testing.T) {
	entry, graph := titledFixture()
	entry.Title = "Daemon removal"
	_, err := llmops.NewDescription(entry, graph, "", false, nil)
	if !errors.Is(err, llmops.ErrDescribed) {
		t.Fatalf("an entry holding title and summary should be refused unless regenerated, got %v", err)
	}
}

func TestPairedDescriptionShowsRelatedTitles(t *testing.T) {
	entry, graph := titledFixture()
	description, err := llmops.NewDescription(unsummarizedCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	req := description.Request
	if !strings.Contains(req.UserPrompt, "Title: daemon project removal") {
		t.Errorf("related entries should carry their titles, so a differing subject can be named apart:\n%s", req.UserPrompt)
	}
	if !strings.Contains(req.UserPrompt, entry.Content) {
		t.Errorf("the paired call should read the full entry:\n%s", req.UserPrompt)
	}
	if !strings.Contains(req.SystemPrompt, `{"title": "...", "summary": "..."}`) {
		t.Errorf("the prompt should ask for the paired answer:\n%s", req.SystemPrompt)
	}
}

func TestTitleDescriptionSendsOnlyWhatATitleNames(t *testing.T) {
	entry, graph := titledFixture()
	related := graph.ByID["20260101-100000-s-tac-rel"]
	description, err := llmops.NewDescription(untitledCopy(entry), graph, "de", false, nil)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	req := description.Request
	combined := req.Combined()
	for _, want := range []string{entry.Summary, "Type: decision", "Layer: tactical", "Kind: directive", related.Title, "`de`"} {
		if !strings.Contains(combined, want) {
			t.Errorf("the title call should carry %q:\n%s", want, combined)
		}
	}
	for _, unwanted := range []string{entry.Content, related.Summary, related.Content, "the ref description"} {
		if strings.Contains(combined, unwanted) {
			t.Errorf("the title call should not carry %q:\n%s", unwanted, combined)
		}
	}
	if strings.Contains(req.SystemPrompt, `"summary"`) {
		t.Errorf("the title-only call should not ask for a summary:\n%s", req.SystemPrompt)
	}
}

func TestDescriptionsStateTheTitleRule(t *testing.T) {
	entry, graph := titledFixture()
	for name, candidate := range map[string]*model.Entry{"title": untitledCopy(entry), "summary": unsummarizedCopy(entry)} {
		t.Run(name, func(t *testing.T) {
			description, err := llmops.NewDescription(candidate, graph, "", false, nil)
			if err != nil {
				t.Fatalf("NewDescription: %v", err)
			}
			if !strings.Contains(description.Request.SystemPrompt, model.TitleRule) {
				t.Errorf("the prompt should state the title rule:\n%s", description.Request.SystemPrompt)
			}
			for _, want := range []string{"uses no quotation marks", "no trailing punctuation but a question mark"} {
				if !strings.Contains(description.Request.SystemPrompt, want) {
					t.Errorf("the prompt should say %q:\n%s", want, description.Request.SystemPrompt)
				}
			}
			if strings.Contains(description.Request.SystemPrompt, "at most 32 characters") {
				t.Errorf("the prompt should no longer cap titles at 32 characters:\n%s", description.Request.SystemPrompt)
			}
		})
	}
}

// Each call constrains its answer to the fields its parser reads; the title-only
// call must not be asked for a summary it would discard.
func TestDescriptionsDeclareTheirSchema(t *testing.T) {
	entry, graph := titledFixture()
	for _, tc := range []struct {
		name    string
		entry   *model.Entry
		want    []string
		without string
	}{
		{name: "summary", entry: unsummarizedCopy(entry), want: []string{`"title"`, `"summary"`}},
		{name: "title", entry: untitledCopy(entry), want: []string{`"title"`}, without: `"summary"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			description, err := llmops.NewDescription(tc.entry, graph, "", false, nil)
			if err != nil {
				t.Fatalf("NewDescription: %v", err)
			}
			schema := string(description.Request.ResponseSchema)
			for _, field := range tc.want {
				if !strings.Contains(schema, field) {
					t.Errorf("the request schema should declare %s, got %s", field, schema)
				}
			}
			if tc.without != "" && strings.Contains(schema, tc.without) {
				t.Errorf("the request schema should not declare %s, got %s", tc.without, schema)
			}
		})
	}
}

func TestPairedDescriptionParsesTheAnswer(t *testing.T) {
	entry, graph := titledFixture()
	description, err := llmops.NewDescription(unsummarizedCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	cases := []struct {
		name        string
		output      string
		wantTitle   string
		wantSummary string
		wantErr     string
	}{
		{name: "paired answer", output: `{"title": "daemon project removal", "summary": "The daemon project is removed."}`, wantTitle: "Daemon project removal", wantSummary: "The daemon project is removed."},
		{name: "answer wrapped in prose", output: "Here it is:\n```json\n{\"title\": \"daemon project removal\", \"summary\": \"The daemon project is removed.\"}\n```", wantTitle: "Daemon project removal", wantSummary: "The daemon project is removed."},
		{name: "trailing period dropped from the title", output: `{"title": "daemon project removal.", "summary": "The daemon goes."}`, wantTitle: "Daemon project removal", wantSummary: "The daemon goes."},
		{name: "trailing punctuation dropped from the title", output: `{"title": "daemon project removal …", "summary": "The daemon goes."}`, wantTitle: "Daemon project removal", wantSummary: "The daemon goes."},
		{name: "a question mark kept", output: `{"title": "should the daemon go?", "summary": "The daemon goes."}`, wantTitle: "Should the daemon go?", wantSummary: "The daemon goes."},
		{name: "only punctuation", output: `{"title": " … ", "summary": "The daemon goes."}`, wantErr: "empty title"},
		{name: "plain prose", output: "The daemon project is removed.", wantErr: "no JSON object"},
		{name: "empty title", output: `{"title": " ", "summary": "The daemon goes."}`, wantErr: "empty title"},
		{name: "multi-line title", output: `{"title": "daemon\nremoval", "summary": "The daemon goes."}`, wantErr: "more than one line"},
		{name: "empty summary", output: `{"title": "daemon project removal", "summary": ""}`, wantErr: "empty summary"},
		{name: "summary of three dots", output: `{"title": "daemon project removal", "summary": "..."}`, wantErr: "empty summary"},
		{name: "summary without a letter or digit", output: `{"title": "daemon project removal", "summary": "— … — !"}`, wantErr: "empty summary"},
		{name: "summary of two words", output: `{"title": "daemon project removal", "summary": "Daemon goes."}`, wantErr: "empty summary"},
		{name: "summary of three words", output: `{"title": "daemon project removal", "summary": "Daemon goes away."}`, wantTitle: "Daemon project removal", wantSummary: "Daemon goes away."},
		{name: "summary of three numbered words", output: `{"title": "daemon project removal", "summary": "Release 0.18 ships."}`, wantTitle: "Daemon project removal", wantSummary: "Release 0.18 ships."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := description.Parse(tc.output)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("the answer should be refused naming its defect, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Title != tc.wantTitle || got.Summary != tc.wantSummary {
				t.Errorf("title and summary should be read from the answer, got %+v", got)
			}
		})
	}
}

func TestTitleDescriptionParsesTheAnswer(t *testing.T) {
	entry, graph := titledFixture()
	description, err := llmops.NewDescription(untitledCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	got, err := description.Parse(`{"title": " daemon project removal "}`)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Title != "Daemon project removal" || got.Summary != "" {
		t.Errorf("the title should be read trimmed and capitalized, leaving the summary alone, got %+v", got)
	}
	_, err = description.Parse(`{"title": ""}`)
	if err == nil {
		t.Error("an empty title should be refused")
	}
}

func TestDescribeGivesTitleAndSummaryFromOneCall(t *testing.T) {
	entry, graph := titledFixture()
	runner := &scriptedRunner{replies: []string{`{"title": "daemon project shutdown", "summary": "The daemon project is removed."}`}}
	got, err := llmops.Describe(context.Background(), runner, unsummarizedCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(runner.requests) != 1 || got.Title == "" || got.Summary == "" {
		t.Errorf("an entry without a summary should get both from one call, got %+v after %d calls", got, len(runner.requests))
	}
}

// A summary drifts when it carries a claim the entry never made, often one
// lent from a related entry; the prompt holds every claim to the entry.
func TestPairedDescriptionClaimsOnlyWhatTheEntrySays(t *testing.T) {
	entry, graph := titledFixture()
	description, err := llmops.NewDescription(unsummarizedCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("NewDescription: %v", err)
	}
	want := "Claim only what the entry itself states: add no cause, consequence, actor or example it does not name, and never lend it a related entry's claims."
	if !strings.Contains(description.Request.SystemPrompt, want) {
		t.Errorf("the prompt should hold every summary claim to the entry:\n%s", description.Request.SystemPrompt)
	}
}

func pairedAnswer(summary string) string {
	return `{"title": "Daemon project goes", "summary": "` + summary + `"}`
}

func sentenceOfWords(n int) string {
	return strings.TrimSpace(strings.Repeat("word ", n)) + "."
}

func TestDescribeRepairsALongFirstSentence(t *testing.T) {
	entry, graph := titledFixture()
	long := sentenceOfWords(llmops.MaxFirstSentenceWords+5) + " It relates to s-tac-rel."
	short := "The daemon project is removed. It relates to s-tac-rel."
	runner := &scriptedRunner{replies: []string{pairedAnswer(long), pairedAnswer(short)}}

	got, err := llmops.Describe(context.Background(), runner, unsummarizedCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got.Summary != short {
		t.Errorf("the repaired summary should be used, got %q", got.Summary)
	}
	if len(runner.requests) != 2 {
		t.Fatalf("a long first sentence should cost exactly one repair call, got %d calls", len(runner.requests))
	}
	repair := runner.requests[1].UserPrompt
	for _, want := range []string{fmt.Sprintf("%d words", llmops.MaxFirstSentenceWords+5), fmt.Sprintf("%d-word", llmops.MaxFirstSentenceWords), long} {
		if !strings.Contains(repair, want) {
			t.Errorf("the repair call should name the first sentence's length, the limit and the answer it repairs (%q):\n%s", want, repair)
		}
	}
}

func TestDescribeGeneratesAFreshSummaryWhenTheRepairLeavesItLong(t *testing.T) {
	entry, graph := titledFixture()
	long := pairedAnswer(sentenceOfWords(llmops.MaxFirstSentenceWords + 1))
	short := "The daemon project is removed."
	runner := &scriptedRunner{replies: []string{long, long, pairedAnswer(short)}}
	var logs bytes.Buffer
	ctx := slogutils.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))

	got, err := llmops.Describe(ctx, runner, unsummarizedCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got.Summary != short {
		t.Errorf("the fresh generation's summary should be used, got %q", got.Summary)
	}
	if len(runner.requests) != 3 {
		t.Fatalf("a repair left too long should cost one fresh generation, got %d calls", len(runner.requests))
	}
	if runner.requests[2].UserPrompt != runner.requests[0].UserPrompt {
		t.Errorf("the fresh generation should send the first call's request again")
	}
	if !strings.Contains(logs.String(), fmt.Sprintf("words=%d", llmops.MaxFirstSentenceWords+1)) {
		t.Errorf("the fresh generation should leave a trace naming the word count:\n%s", logs.String())
	}
}

func TestDescribeRefusesAFirstSentenceTheFreshGenerationLeavesLong(t *testing.T) {
	entry, graph := titledFixture()
	long := pairedAnswer(sentenceOfWords(llmops.MaxFirstSentenceWords + 1))
	runner := &scriptedRunner{replies: []string{long, long, long, long}}

	_, err := llmops.Describe(context.Background(), runner, unsummarizedCopy(entry), graph, "", false, nil)
	if !errors.Is(err, llmops.ErrLongFirstSentence) {
		t.Fatalf("a first sentence still too long after the fresh generation should fail the description, got %v", err)
	}
	if !strings.Contains(err.Error(), "publishing again generates a new summary") {
		t.Errorf("the error should say that trying again can help, got %v", err)
	}
	if len(runner.requests) != 4 {
		t.Errorf("one fresh generation with its own repair call should be made, got %d calls", len(runner.requests))
	}
}

func TestDescribeRegeneratesAnEmptySummary(t *testing.T) {
	entry, graph := titledFixture()
	full := "The daemon project is removed."
	runner := &scriptedRunner{replies: []string{pairedAnswer("..."), pairedAnswer(full)}}

	got, err := llmops.Describe(context.Background(), runner, unsummarizedCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got.Summary != full {
		t.Errorf("the regenerated summary should be used, got %q", got.Summary)
	}
	if len(runner.requests) != 2 {
		t.Fatalf("an empty summary should cost exactly one repair call, got %d calls", len(runner.requests))
	}
	repair := runner.requests[1].UserPrompt
	for _, want := range []string{pairedAnswer("..."), fmt.Sprintf("fewer than %d words", llmops.MinSummaryWords)} {
		if !strings.Contains(repair, want) {
			t.Errorf("the repair call should carry the answer it repairs and name the defect (%q):\n%s", want, repair)
		}
	}
}

func TestDescribeRefusesASummaryTheRepairLeavesEmpty(t *testing.T) {
	entry, graph := titledFixture()
	runner := &scriptedRunner{replies: []string{pairedAnswer("..."), pairedAnswer("Removed.")}}

	_, err := llmops.Describe(context.Background(), runner, unsummarizedCopy(entry), graph, "", false, nil)
	if !errors.Is(err, llmops.ErrEmptySummary) {
		t.Fatalf("a summary still empty after the repair should fail the description, got %v", err)
	}
	if len(runner.requests) != 2 {
		t.Errorf("only one repair call should be made, got %d calls", len(runner.requests))
	}
}

func TestDescribeLeavesAFirstSentenceAtTheLimitAlone(t *testing.T) {
	entry, graph := titledFixture()
	runner := &scriptedRunner{replies: []string{pairedAnswer(sentenceOfWords(llmops.MaxFirstSentenceWords))}}

	_, err := llmops.Describe(context.Background(), runner, unsummarizedCopy(entry), graph, "", false, nil)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(runner.requests) != 1 {
		t.Errorf("a first sentence within the limit should need no repair call, got %d calls", len(runner.requests))
	}
}

const similarHeading = "## Titles of the most similar entries"

const similarRule = "The new title must not conflict with any of these titles: it must not repeat one or be mistakable for one. Name what sets this entry apart."

// listedSimilarTitles reads the similar-titles list out of a prompt, nil when
// the prompt has no such section.
func listedSimilarTitles(prompt string) []string {
	_, section, ok := strings.Cut(prompt, similarHeading)
	if !ok {
		return nil
	}
	var titles []string
	for _, line := range strings.Split(section, "\n") {
		if title, ok := strings.CutPrefix(line, "- "); ok {
			titles = append(titles, title)
		}
	}
	return titles
}

func TestDescriptionsListSimilarTitles(t *testing.T) {
	entry, graph := titledFixture()
	similar := []string{"Search index layout", "Title backfill batches"}
	for name, candidate := range map[string]*model.Entry{"title": untitledCopy(entry), "summary": unsummarizedCopy(entry)} {
		t.Run(name, func(t *testing.T) {
			with, err := llmops.NewDescription(candidate, graph, "", false, similar)
			if err != nil {
				t.Fatalf("NewDescription: %v", err)
			}
			prompt := with.Request.UserPrompt
			if !strings.Contains(prompt, similarRule) {
				t.Errorf("the prompt should forbid a title conflicting with the similar ones:\n%s", prompt)
			}
			if got := listedSimilarTitles(prompt); !slices.Equal(got, similar) {
				t.Errorf("the prompt should list the similar titles in rank order, got %q:\n%s", got, prompt)
			}
			if !strings.Contains(prompt, "Title: daemon project removal") && !strings.Contains(prompt, ": daemon project removal") {
				t.Errorf("the related entries should still be listed:\n%s", prompt)
			}

			without, err := llmops.NewDescription(candidate, graph, "", false, nil)
			if err != nil {
				t.Fatalf("NewDescription: %v", err)
			}
			combined := without.Request.Combined()
			if strings.Contains(combined, similarHeading) || strings.Contains(combined, similarRule) {
				t.Errorf("without similar titles the prompt should carry neither their section nor their rule:\n%s", combined)
			}
		})
	}
}

// repeatFixture is a graph whose entries hold titles the description's answer
// can repeat; the entry described holds the title "Index segment cache".
func repeatFixture() (*model.Entry, *model.Graph) {
	entry, graph := titledFixture()
	entry.Title = "Index segment cache"
	other := &model.Entry{
		ID: "20260103-100000-s-tac-oth", Type: model.TypeSignal, Kind: model.KindGap, Layer: model.LayerTactical,
		Title: "Daemon shutdown order", Summary: "The daemon stops in the wrong order.", Content: "Order matters.",
	}
	return entry, model.NewGraph(append(slices.Clone(graph.Entries), other))
}

func TestDescribeRerunsARepeatedTitleOnce(t *testing.T) {
	previous := []string{"First", "Second", "Third", "Fourth", "Fifth"}
	cases := []struct {
		name      string
		similar   []string
		replies   []string
		wantList  []string
		wantTitle string
	}{
		{
			name:      "repeated title outside the list",
			similar:   previous,
			replies:   []string{`{"title": " daemon SHUTDOWN order ", "summary": "The daemon stops first."}`, `{"title": "Daemon stop sequence"}`},
			wantList:  []string{"Daemon shutdown order", "First", "Second", "Third", "Fourth"},
			wantTitle: "Daemon stop sequence",
		},
		{
			name:      "repeated title inside the list",
			similar:   []string{"First", "Daemon shutdown order", "Third", "Fourth", "Fifth"},
			replies:   []string{`{"title": "Daemon shutdown order", "summary": "The daemon stops first."}`, `{"title": "Daemon stop sequence"}`},
			wantList:  []string{"Daemon shutdown order", "First", "Third", "Fourth", "Fifth"},
			wantTitle: "Daemon stop sequence",
		},
		{
			name:      "re-run repeats again",
			similar:   previous,
			replies:   []string{`{"title": "Daemon shutdown order", "summary": "The daemon stops first."}`, `{"title": "daemon project removal"}`},
			wantList:  []string{"Daemon shutdown order", "First", "Second", "Third", "Fourth"},
			wantTitle: "Daemon project removal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, graph := repeatFixture()
			runner := &scriptedRunner{replies: tc.replies}
			got, err := llmops.Describe(context.Background(), runner, unsummarizedCopy(entry), graph, "", false, tc.similar)
			if err != nil {
				t.Fatalf("Describe: %v", err)
			}
			if len(runner.requests) != 2 {
				t.Fatalf("a repeated title should be re-run exactly once, got %d calls", len(runner.requests))
			}
			rerun := runner.requests[1]
			if rerun.Purpose != llm.PurposeTitle {
				t.Errorf("the re-run should be the title-only call, got %q", rerun.Purpose)
			}
			if !strings.Contains(rerun.UserPrompt, "The daemon stops first.") {
				t.Errorf("the re-run should title the summary the paired call wrote:\n%s", rerun.UserPrompt)
			}
			if list := listedSimilarTitles(rerun.UserPrompt); !slices.Equal(list, tc.wantList) {
				t.Errorf("the re-run should list the repeated title first, then the earlier titles without the last, got %q", list)
			}
			if got.Title != tc.wantTitle || got.Summary != "The daemon stops first." {
				t.Errorf("the re-run's title should be stored beside the paired call's summary, got %+v", got)
			}
		})
	}
}

func TestDescribeKeepsATitleNoOtherEntryHolds(t *testing.T) {
	cases := []struct {
		name       string
		entry      func(*model.Entry) *model.Entry
		regenerate bool
		reply      string
	}{
		{name: "free title", entry: unsummarizedCopy, reply: `{"title": "Daemon stop sequence", "summary": "The daemon goes."}`},
		{name: "the entry's own title", entry: func(e *model.Entry) *model.Entry { return e }, regenerate: true, reply: `{"title": "index segment cache", "summary": "The daemon goes."}`},
		{name: "a title the caller keeps", entry: func(e *model.Entry) *model.Entry {
			kept := unsummarizedCopy(e)
			kept.Title = "Caller title"
			return kept
		}, reply: `{"title": "Daemon shutdown order", "summary": "The daemon goes."}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, graph := repeatFixture()
			runner := &scriptedRunner{replies: []string{tc.reply, `{"title": "Another title"}`}}
			_, err := llmops.Describe(context.Background(), runner, tc.entry(entry), graph, "", tc.regenerate, []string{"First"})
			if err != nil {
				t.Fatalf("Describe: %v", err)
			}
			if len(runner.requests) != 1 {
				t.Errorf("a title repeating no other entry's title should not be re-run, got %d calls", len(runner.requests))
			}
		})
	}
}

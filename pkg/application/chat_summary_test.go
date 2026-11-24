package application_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const (
	unsummarized     = "20260101-100008-s-tac-nos"
	unsummarizedBody = "Nobody summarized these alpha matters."
)

// writeUnsummarized writes a gap without a summary, its body naming alpha
// so a draft about alpha finds it.
func (f *gatherFixture) writeUnsummarized(t *testing.T, id string) {
	t.Helper()
	dir := filepath.Join(f.graphDir, id[:4], id[4:6])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\n---\n\n" + unsummarizedBody + "\n"
	if err := os.WriteFile(filepath.Join(dir, id[6:]+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// chatModel answers every chat judge call with score for each label and
// keeps the candidate lines it was shown.
type chatModel struct {
	mu    sync.Mutex
	score float64
	shown []string
}

var chatLabel = regexp.MustCompile(`(?m)^(c\d+) (.*)$`)

func (m *chatModel) Run(_ context.Context, request pkgllm.Request) (pkgllm.Result, error) {
	_, candidates, _ := strings.Cut(request.UserPrompt, "Candidates:")
	var scores []string
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, match := range chatLabel.FindAllStringSubmatch(candidates, -1) {
		scores = append(scores, fmt.Sprintf("%q: %v", match[1], m.score))
		m.shown = append(m.shown, match[2])
	}
	return pkgllm.Result{Text: `{"scores": {` + strings.Join(scores, ", ") + `}}`}, nil
}

// showedTheUnsummarized reports whether a call listed the entry without a
// summary.
func (m *chatModel) showedTheUnsummarized() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.shown, func(line string) bool { return strings.Contains(line, unsummarizedBody) })
}

func chatJudge(t *testing.T, chat *chatModel) sdd.Prefilter {
	t.Helper()
	judge, err := llmops.NewChatJudge(chat)
	if err != nil {
		t.Fatal(err)
	}
	return sdd.Prefilter{Scorer: judge, Judge: gatherJudge}
}

// A chat judge reads an entry by its summary line: an entry without a
// summary goes into no call and stays a line to judge, unscored, never set
// aside, and the pass names it apart from any failure.
func TestGather_ChatJudgeLeavesAnEntryWithoutASummaryUnscored(t *testing.T) {
	chat := &chatModel{score: 0}
	f := newGatherFixture(t)
	f.seed(t)
	f.writeUnsummarized(t, unsummarized)
	f.prefilter = chatJudge(t, chat)
	f.relevance = calibratedLedger(t)
	application := f.app(t, prefilterPolicy(1), nil)
	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha matters."})
	if err != nil {
		t.Fatal(err)
	}
	if len(chat.shown) == 0 || chat.showedTheUnsummarized() {
		t.Fatalf("the chat judge should score the summarized entries and not be shown the one without a summary: %v", chat.shown)
	}
	line := lineOf(result.Text, unsummarized)
	if line == "" || strings.Contains(line, "score") || strings.Contains(setAsideLine(result.Text, model.SetAsideFiltered), unsummarized) {
		t.Fatalf("the entry without a summary should stay a line to judge, unscored, never set aside:\n%s", result.Text)
	}
	note := prefilterNote(result.Text)
	if !strings.HasPrefix(note, "prefilter: ok") || !strings.Contains(note, "1 the judge cannot read, left to judge unscored") {
		t.Errorf("the note should count the entry the judge cannot read, not a failure: %s", note)
	}
}

// Re-scoring sends no pair whose candidate lacks a summary to a chat judge;
// the pair stays unscored and is counted apart.
func TestRescoreRelevance_ChatJudgeLeavesAPairWithoutASummaryUnscored(t *testing.T) {
	chat := &chatModel{score: 0.5}
	f := newGatherFixture(t)
	subject := "20260201-000000-d-tac-sub"
	readable := "20260101-000000-s-tac-pos"
	f.write(t, subject, "directive", "The subject decision.", "intent: pending\n")
	f.write(t, readable, "gap", "A referenced gap.", "")
	f.writeUnsummarized(t, unsummarized)
	var batch model.RelevanceBatch
	for _, id := range []string{readable, unsummarized} {
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: model.NewRelevancePair(id, subject), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	f.relevance = store
	f.prefilter = chatJudge(t, chat)
	application := f.app(t, prefilterPolicy(1), nil)
	result, err := application.RescoreRelevance(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if chat.showedTheUnsummarized() {
		t.Fatal("the chat judge should not be shown a candidate without a summary")
	}
	if result.Scored != 1 || result.Unread != 1 {
		t.Fatalf("result %+v: the readable pair should be scored, the other counted unread", result)
	}
}

// `sdd grounding status` counts no labelled pair the judge cannot read as
// unscored, so it suggests no rescore that could not score it.
func TestGroundingStatus_CountsNoPairTheJudgeCannotReadAsUnscored(t *testing.T) {
	f := newGatherFixture(t)
	subject := "20260201-000000-d-tac-sub"
	var batch model.RelevanceBatch
	for i := range 40 {
		scored := model.NewRelevancePair(fmt.Sprintf("20250101-%06d-s-tac-pos", i), subject)
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: scored, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: scored, Score: model.FilterScore{Judge: gatherJudge, Score: 200}})
		id := fmt.Sprintf("20260102-%06d-s-tac-nos", i)
		f.writeUnsummarized(t, id)
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: model.NewRelevancePair(id, subject), Verdict: model.ReaderVerdict{Code: model.ReaderReferenced}})
	}
	store := localadapter.NewMemoryRelevanceStore()
	if err := store.Append(t.Context(), relevance.Encode(batch)); err != nil {
		t.Fatal(err)
	}
	f.relevance = store
	f.prefilter = chatJudge(t, &chatModel{})
	application := f.app(t, prefilterPolicy(1), nil)
	status, err := application.GroundingStatus(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject, sdd.GroundingStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(status.Text, "unscored") {
		t.Fatalf("pairs the judge cannot read should not count as unscored:\n%s", status.Text)
	}
}

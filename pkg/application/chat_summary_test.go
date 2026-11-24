package application_test

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

const unsummarized = "20260101-100008-s-tac-nos"

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
// summary, whose one sentence is the fixture's mark.
func (m *chatModel) showedTheUnsummarized() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.ContainsFunc(m.shown, func(line string) bool { return strings.Contains(line, fixtureMark) })
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
// aside, with the pass neither failed nor short of its minimum.
func TestGather_ChatJudgeLeavesAnEntryWithoutASummaryUnscored(t *testing.T) {
	chat := &chatModel{score: 0}
	f := newGatherFixture(t)
	f.seed(t)
	f.write(t, unsummarized, "gap", "", "")
	f.prefilter = chatJudge(t, chat)
	f.relevance, f.proofs = calibratedLedger(t), provenThreshold(t)
	policy := prefilterPolicy(1)
	policy.JudgeMinimum = 50
	application := f.app(t, policy, nil)
	result, err := gather(t, application, sdd.GatherRequest{Draft: "A draft about alpha."})
	if err != nil {
		t.Fatal(err)
	}
	if chat.showedTheUnsummarized() {
		t.Fatal("the chat judge should not be shown an entry without a summary")
	}
	if line := lineOf(result.Text, unsummarized); line == "" || !strings.HasPrefix(line[1:], " ?") {
		t.Fatalf("the entry without a summary should stay a line to judge, unscored:\n%s", result.Text)
	}
	if setAsideCount(result.Text, model.SetAsideFiltered) == 0 {
		t.Fatalf("the scored entries should still be set aside below the threshold:\n%s", result.Text)
	}
	note := prefilterNote(result.Text)
	if !strings.HasPrefix(note, "prefilter: ok") || !strings.Contains(note, "1 the judge cannot read, left to judge unscored") {
		t.Errorf("the note should count the entry the judge cannot read, not a failure: %s", note)
	}
	if strings.Contains(result.Text, "judged, below the") {
		t.Errorf("an entry the judge cannot read should not leave the check short of its minimum:\n%s", result.Text)
	}
}

// Re-scoring sends no pair whose candidate lacks a summary to a chat judge;
// the pair stays unscored without failing its subject.
func TestUpdateGrounding_ChatJudgeLeavesAPairWithoutASummaryUnscored(t *testing.T) {
	chat := &chatModel{score: 0.5}
	f := newGatherFixture(t)
	subject := "20260201-000000-d-tac-sub"
	readable := "20260101-000000-s-tac-pos"
	f.write(t, subject, "directive", "The subject decision.", "intent: pending\n")
	f.write(t, readable, "gap", "A referenced gap.", "")
	f.write(t, unsummarized, "gap", "", "")
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
	updated, err := application.UpdateGrounding(t.Context(), sdd.RequestIdentity{Subject: "tester"}, gatherProject, sdd.GroundingUpdateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if chat.showedTheUnsummarized() {
		t.Fatal("the chat judge should not be shown a candidate without a summary")
	}
	if result := updated.Scored; result.Scored != 1 || result.Unread != 1 || result.Failed != 0 || len(result.Failures) != 0 {
		t.Fatalf("result %+v: the readable pair should be scored, the other counted unread and no subject fail", result)
	}
}

// lockedLog is a log sink safe for the calibrator's goroutines.
type lockedLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) count(message string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.b.String(), message)
}

// Background calibration sends no candidate the judge cannot read, and a
// round that leaves every subject unscored for that reason is not run
// again by the same server.
func TestAutoCalibrate_RunsOnceOverSubjectsTheJudgeCannotRead(t *testing.T) {
	f := newAutoFixture(t)
	scorer := &scriptedScorer{blind: map[string]bool{}}
	for s := range autoSubjects {
		for i := range autoRefs {
			scorer.blind[autoRef(s, i)] = true
		}
	}
	application := f.application(t, scorer, true)
	logs := &lockedLog{}
	ctx, cancel := context.WithCancel(slogutils.WithLogger(t.Context(), slog.New(slog.NewTextHandler(logs, nil))))
	done := make(chan struct{})
	go func() {
		defer close(done)
		application.AutoCalibrate(ctx, updateIdentity, gatherProject, sdd.AutoCalibrationRequest{})
	}()
	eventually(t, "a round finishes", func() bool { return logs.count("auto-calibration finished its round") > 0 })
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done
	if scorer.calls() != 0 {
		t.Fatalf("%d calls: no request should carry a candidate the judge cannot read", scorer.calls())
	}
	if rounds := logs.count("auto-calibration started"); rounds != 1 {
		t.Fatalf("%d rounds: subjects the judge cannot read should be left until the next server", rounds)
	}
}

// A replay holds no candidate the judge cannot read: it was never scored,
// so it labels nothing and is never the planted miss.
func TestAutoCalibrate_ReplaysLeaveOutCandidatesTheJudgeCannotRead(t *testing.T) {
	f := replayFixture(t)
	proofs := localadapter.NewMemoryProofStore()
	f.proofs = proofs
	scorer := &scriptedScorer{blind: map[string]bool{}}
	for s := range replaySubjects {
		scorer.blind[autoRef(s, 1)] = true
	}
	f.prefilter = sdd.Prefilter{Scorer: scorer, Judge: gatherJudge, Revision: "r1", Scan: &flaggingScan{}}
	policy := prefilterPolicy(1)
	policy.AutoCalibrate = true
	application := f.app(t, policy, &countingEmbeddings{})
	startAutoCalibrate(t, application, sdd.AutoCalibrationRequest{})

	deadline := time.Now().Add(90 * time.Second)
	for len(proofRecordsOf(t, proofs).Replays) < replaySubjects {
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d subjects replayed in time", len(proofRecordsOf(t, proofs).Replays), replaySubjects)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, check := range proofRecordsOf(t, proofs).Replays {
		for _, candidate := range check.Candidates {
			if scorer.blind[candidate.ID] {
				t.Fatalf("check of %s holds %s, which the judge cannot read", check.Subject, candidate.ID)
			}
		}
		if scorer.blind[check.Planted] {
			t.Fatalf("check of %s plants %s, which the judge cannot read", check.Subject, check.Planted)
		}
	}
	for id := range scorer.blind {
		if scorer.carried(id) {
			t.Fatalf("a request carried %s, which the judge cannot read", id)
		}
	}
}

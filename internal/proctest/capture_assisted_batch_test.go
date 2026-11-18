package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// One answer of the person settles several sealed drafts: the agent answers
// one draft's playback and names the others under batch, by the choice each
// takes.

func sealedDrafts(t *testing.T, session *proctest.Session, n int) []string {
	t.Helper()
	instances := make([]string, 0, n)
	for i := range n {
		draft := captureDraft()
		draft["body"] = []string{"A tactical gap: the first fixture observes something.", "A tactical gap: the second fixture observes something.", "A tactical gap: the third fixture observes something."}[i]
		instances = append(instances, sealedCapture(t, session, draft))
	}
	return instances
}

func batchConfirm(t *testing.T, session *proctest.Session, instance, words string, batch map[string]any) (*sdd.WorkflowServe, error) {
	t.Helper()
	return session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: map[string]any{
		"chooser": "playback", "choice": "confirm", "userWords": words, "batch": batch,
	}})
}

func TestAssistedBatch_TheGateNamesTheOtherSealedDrafts(t *testing.T) {
	_, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instances := sealedDrafts(t, session, 2)
	serve, err := session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instances[1]})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(serve.Instructions, instances[0]) || !strings.Contains(serve.Instructions, "batch") {
		t.Errorf("the playback should name the other sealed draft and how one answer settles both, got:\n%s", serve.Instructions)
	}
}

func TestAssistedBatch_OneAnswerApprovesAllButTheExcluded(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instances := sealedDrafts(t, session, 3)
	serve, err := batchConfirm(t, session, instances[0], "all except 3", map[string]any{"confirm": []any{instances[1]}, "adjust": []any{instances[2]}})
	if err != nil {
		t.Fatal(err)
	}
	proctest.RequireStep(t, serve, "verifySummary")
	for _, want := range []string{instances[1] + " (confirm) now at verifySummary", instances[2] + " (adjust) now at assemble"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("the serve should report what the same answer did, missing %q in:\n%s", want, serve.Instructions)
		}
	}
	for _, instance := range instances[:2] {
		entry := writeCapture(t, world, session, instance, func() *sdd.WorkflowServe {
			serve, err := session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instance})
			if err != nil {
				t.Fatal(err)
			}
			return serve
		})
		if entry.ApprovedBy != "Ada" {
			t.Errorf("each draft the answer approves should be written as the person's, got approved-by %q", entry.ApprovedBy)
		}
	}

	// The excluded draft waits at assemble for its revision, then plays back again.
	serve, err = session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instances[2]})
	if err != nil {
		t.Fatal(err)
	}
	proctest.RequireStep(t, serve, "assemble")
	if !hasDiagnostic(serve, "sent this draft back") {
		t.Errorf("the excluded draft should hold for its revision, got diagnostics %v", serve.Diagnostics)
	}
	serve = session.Report(t, instances[2], map[string]any{"body": "A tactical gap: the third fixture observes something twice."})
	proctest.RequireStep(t, serve, "playback")
}

func TestAssistedBatch_RefusesWhatIsNotSealed(t *testing.T) {
	cases := []struct {
		name  string
		batch func(instances []string, unsealed string) map[string]any
		want  string
	}{
		{name: "a draft before its gate", batch: func(_ []string, unsealed string) map[string]any { return map[string]any{"confirm": []any{unsealed}} }, want: "not sealed"},
		{name: "the answered draft itself", batch: func(instances []string, _ string) map[string]any {
			return map[string]any{"confirm": []any{instances[0]}}
		}, want: "names the draft it answers"},
		{name: "a draft named twice", batch: func(instances []string, _ string) map[string]any {
			return map[string]any{"confirm": []any{instances[1]}, "abort": []any{instances[1]}}
		}, want: "more than once"},
		{name: "a choice the gate does not offer", batch: func(instances []string, _ string) map[string]any { return map[string]any{"skip": []any{instances[1]}} }, want: "not an option"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
			instances := sealedDrafts(t, session, 2)
			unsealed := session.Start(t, "capture", nil).Instance
			_, err := batchConfirm(t, session, instances[0], "all of them", tc.batch(instances, unsealed))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the batch should be refused saying %q, got %v", tc.want, err)
			}
			if got := world.LLM.Calls("summarize"); got != 0 {
				t.Errorf("a refused batch should write nothing, got %d summaries", got)
			}
		})
	}
}

func TestAssistedBatch_OnlyAtTheApprovalGate(t *testing.T) {
	_, session := authorshipWorld(t, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	serve = session.Report(t, serve.Instance, captureDraft())
	proctest.RequireStep(t, serve, "playback")
	_, err := session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: serve.Instance, Report: map[string]any{
		"chooser": "playback", "choice": "confirm", "reason": "the draft holds", "batch": map[string]any{"confirm": []any{"x"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "only at an assisted session's approval gate") {
		t.Fatalf("a batch outside an assisted approval gate should be refused, got %v", err)
	}
}

// sealedDecisions drives n assisted directive captures to their gate.
func sealedDecisions(t *testing.T, session *proctest.Session, n int) []string {
	t.Helper()
	instances := make([]string, 0, n)
	for i := range n {
		draft := assistedDirective()
		draft["body"] = []string{"Guiding directive: the first fixture retries once.", "Guiding directive: the second fixture retries once.", "Guiding directive: the third fixture retries once."}[i]
		instances = append(instances, sealedCapture(t, session, draft))
	}
	return instances
}

// One approval adopts each listed draft's own drafted reason as the person's.
func TestAssistedBatch_EachDraftedReasonBecomesThePersons(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instances := sealedDecisions(t, session, 3)
	serve, err := batchConfirm(t, session, instances[0], "ok all three", map[string]any{"confirm": []any{instances[1], instances[2]}})
	if err != nil {
		t.Fatal(err)
	}
	proctest.RequireStep(t, serve, "verifySummary")
	for i := range instances {
		entry := writeCapture(t, world, session, instances[i], func() *sdd.WorkflowServe {
			serve, err := session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instances[i]})
			if err != nil {
				t.Fatal(err)
			}
			return serve
		})
		if entry.Reason != assistedReason || entry.ReasonBy != "Ada" || entry.ApprovedBy != "Ada" {
			t.Errorf("draft %d should carry its drafted reason as the person's, got %q by %q approved-by %q", i+1, entry.Reason, entry.ReasonBy, entry.ApprovedBy)
		}
	}
}

// A batch lists sealed drafts by instance id alone, and a malformed one is
// refused before any draft takes the answer.
func TestAssistedBatch_RefusesAnItemThatIsNotAnInstanceID(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instances := sealedDecisions(t, session, 2)
	_, err := batchConfirm(t, session, instances[0], "ok, flaky", map[string]any{
		"confirm": []any{map[string]any{"instance": instances[1], "reasonWords": "flaky"}},
	})
	if err == nil || !strings.Contains(err.Error(), "as its instance id") {
		t.Fatalf("an item carrying words should be refused, got %v", err)
	}
	if got := world.LLM.Calls("summarize"); got != 0 {
		t.Errorf("a refused batch should write nothing, got %d summaries", got)
	}
	for _, instance := range instances {
		serve, err := session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instance})
		if err != nil {
			t.Fatal(err)
		}
		proctest.RequireStep(t, serve, "playback")
	}
}

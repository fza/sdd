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

func batchAnswer(t *testing.T, session *proctest.Session, instance, choice, words string, batch map[string]any) (*sdd.WorkflowServe, error) {
	t.Helper()
	return session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: map[string]any{
		"chooser": "playback", "choice": choice, "userWords": words, "batch": batch,
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
	serve, err := batchAnswer(t, session, instances[0], "confirm", "all except 3", map[string]any{"confirm": []any{instances[1]}, "adjust": []any{instances[2]}})
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
			_, err := batchAnswer(t, session, instances[0], "confirm", "all of them", tc.batch(instances, unsealed))
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

func batchAnswerWith(t *testing.T, session *proctest.Session, instance, choice, words string, fields, batch map[string]any) (*sdd.WorkflowServe, error) {
	t.Helper()
	report := map[string]any{"chooser": "playback", "choice": choice, "userWords": words, "batch": batch}
	if fields != nil {
		report["fields"] = fields
	}
	return session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: report})
}

// One reason the person gives applies to every draft listed under the same
// choice, and a listed draft may carry a reason of its own.
func TestAssistedBatch_TheReasonsAreThePersons(t *testing.T) {
	world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
	instances := sealedDecisions(t, session, 3)
	const shared, own = "every suite flakes the same way", "the second suite runs nightly"
	words := "ok\nOne reason for all: " + shared + "\nexcept 2: " + own
	serve, err := batchAnswerWith(t, session, instances[0], "confirm", words, map[string]any{"reasonWords": shared},
		map[string]any{"confirm": []any{map[string]any{"instance": instances[1], "reasonWords": own}, instances[2]}})
	if err != nil {
		t.Fatal(err)
	}
	proctest.RequireStep(t, serve, "verifySummary")
	for i, want := range []string{shared, own, shared} {
		entry := writeCapture(t, world, session, instances[i], func() *sdd.WorkflowServe {
			serve, err := session.WF.Advance(t.Context(), session.World.Identity, sdd.WorkflowAdvanceRequest{Instance: instances[i]})
			if err != nil {
				t.Fatal(err)
			}
			return serve
		})
		if entry.Reason != want || entry.ReasonBy != "Ada" || entry.ApprovedBy != "Ada" {
			t.Errorf("draft %d should carry the person's reason %q, got %q by %q approved-by %q", i+1, want, entry.Reason, entry.ReasonBy, entry.ApprovedBy)
		}
	}
}

// A batch missing a reason, or carrying a malformed item, is refused before
// any draft takes the answer.
func TestAssistedBatch_RefusesAnApprovalWithoutTheReason(t *testing.T) {
	cases := []struct {
		name   string
		choice string
		words  string
		fields map[string]any
		batch  func(instances []string) map[string]any
		want   string
	}{
		{name: "no reason at all", words: "ok all", batch: func(in []string) map[string]any { return map[string]any{"confirm": []any{in[1]}} }, want: "without the person's reason"},
		{name: "a listed decision without one", choice: "adjust", words: "change 1 because it is flaky, take 2", fields: map[string]any{"reasonWords": "it is flaky"},
			batch: func(in []string) map[string]any { return map[string]any{"confirm": []any{in[1]}} }, want: "without the person's reason"},
		{name: "a reason the person did not give", words: "ok, flaky", fields: map[string]any{"reasonWords": "flaky"},
			batch: func(in []string) map[string]any {
				return map[string]any{"confirm": []any{map[string]any{"instance": in[1], "reasonWords": "it saves time"}}}
			}, want: "must quote the user's words"},
		{name: "an item with a field it cannot carry", words: "ok, flaky", fields: map[string]any{"reasonWords": "flaky"},
			batch: func(in []string) map[string]any {
				return map[string]any{"confirm": []any{map[string]any{"instance": in[1], "body": "x"}}}
			}, want: `carries "body"`},
		{name: "words on a choice that takes none", words: "ok, flaky; drop 2", fields: map[string]any{"reasonWords": "flaky"},
			batch: func(in []string) map[string]any {
				return map[string]any{"abort": []any{map[string]any{"instance": in[1], "reasonWords": "flaky"}}}
			}, want: "takes no reasonWords"},
		{name: "an item without its instance", words: "ok, flaky", fields: map[string]any{"reasonWords": "flaky"},
			batch: func(in []string) map[string]any {
				return map[string]any{"confirm": []any{map[string]any{"reasonWords": "flaky"}}}
			}, want: "names no instance"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			world, session := authorshipWorld(t, proctest.WithAssistedDialogue())
			instances := sealedDecisions(t, session, 3)
			choice := tc.choice
			if choice == "" {
				choice = "confirm"
			}
			_, err := batchAnswerWith(t, session, instances[0], choice, tc.words, tc.fields, tc.batch(instances))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the batch should be refused saying %q, got %v", tc.want, err)
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
		})
	}
}

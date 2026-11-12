// Capture with the deep draft verifier (20261109-004729-d-cpt-xs5, slice 3):
// once pre-flight leaves no high finding open, the capture dispatches the
// verify task into a delegate child session, exchanges findings, answers and
// responses with it, and writes only under a deep verdict or the user's skip
// covering the current draft.
package proctest_test

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/test"
)

const deepBody = "A tactical gap: the fixture observes something."

func newDeepWorld(t *testing.T, connID string, policy sdd.VerifierPolicy, options ...proctest.Option) (*proctest.World, *proctest.Session) {
	t.Helper()
	policy.Mode = "deep"
	return newCaptureWorld(t, connID, append([]proctest.Option{proctest.WithGroundingEnforced(),
		proctest.WithVerifier(policy, 200000, 4096)}, options...)...)
}

// deepExchange is one capture driven to the deep exchange, with its verify
// delegate.
type deepExchange struct {
	world    *proctest.World
	session  *proctest.Session
	instance string
	dispatch *sdd.WorkflowDispatch
	delegate *proctest.Session
	child    string
}

// confirmedDeepCapture drafts, confirms the playback and passes pre-flight,
// reaching the deep exchange's dispatch.
func confirmedDeepCapture(t *testing.T, session *proctest.Session, draft map[string]any) (string, *sdd.WorkflowServe) {
	t.Helper()
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "playback")
	return instance, session.Answer(t, instance, "playback", "confirm", nil, "write it")
}

// dispatchDeep answers the dispatch, which forks the verify delegate.
func dispatchDeep(t *testing.T, world *proctest.World, session *proctest.Session, instance string) *deepExchange {
	t.Helper()
	serve := session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	proctest.RequireStep(t, serve, "deepLaunch")
	if len(serve.Dispatches) != 1 {
		t.Fatalf("the dispatching answer should owe one dispatch, got %+v", serve.Dispatches)
	}
	x := &deepExchange{world: world, session: session, instance: instance, dispatch: serve.Dispatches[0]}
	x.delegate = world.Delegate(t, x.dispatch)
	x.child = x.dispatch.Instance
	return x
}

// startDeep reaches the dispatch and answers it, forking the verify delegate.
func startDeep(t *testing.T, world *proctest.World, session *proctest.Session, draft map[string]any) *deepExchange {
	t.Helper()
	instance, serve := confirmedDeepCapture(t, session, draft)
	proctest.RequireStep(t, serve, "deepDispatch")
	return dispatchDeep(t, world, session, instance)
}

// judge serves the delegate every page and reports its findings.
func (x *deepExchange) judge(t *testing.T, findings []any) *sdd.WorkflowServe {
	t.Helper()
	readAllPages(t, x.delegate, x.child)
	return x.delegate.Answer(t, x.child, "read", "judge", map[string]any{"findings": findings}, "")
}

// readAllPages turns the verifier's reading to its last page.
func readAllPages(t *testing.T, delegate *proctest.Session, instance string) {
	t.Helper()
	for range 20 {
		serve := delegate.Answer(t, instance, "read", "more", nil, "")
		proctest.RequireStep(t, serve, "read")
		if strings.Contains(serve.Instructions, "Every page is served.") {
			return
		}
	}
	t.Fatal("the reading should end within 20 pages")
}

// pickUp reports the launch started and picks the delegate's delivery up.
func (x *deepExchange) pickUp(t *testing.T) *sdd.WorkflowServe {
	t.Helper()
	x.session.Refresh(t)
	return x.session.Answer(t, x.instance, "deepLaunch", "started", nil, "")
}

func deepEdit(old, new string) map[string]any {
	return map[string]any{"old": old, "new": new}
}

func TestCaptureDeep_ACleanVerdictWrites(t *testing.T) {
	world, session := newDeepWorld(t, "deep-clean", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	for _, prompt := range []string{x.dispatch.Prompt, x.dispatch.ContinuePrompt} {
		if strings.Contains(prompt, string(session.ID)) || !strings.Contains(prompt, string(x.dispatch.Session)) {
			t.Fatalf("a delegate prompt carries the child handle and never the capture's, got %q", prompt)
		}
	}
	serve := x.judge(t, []any{})
	proctest.RequireStep(t, serve, "waitAnswers")
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, x.instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); entry.Verification != "" || entry.Confidence != "medium" {
		t.Fatalf("a verified entry carries no skip marker and keeps its confidence, got %q and %q", entry.Verification, entry.Confidence)
	}
}

func TestCaptureDeep_ServesTheReadingInPagesAndRefusesEarlyFindings(t *testing.T) {
	world, session := newDeepWorld(t, "deep-pages", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	_, resume := world.Resume(t, x.dispatch.Session, "verifier")
	first := resume.Open[0].Instructions
	x.delegate.Refresh(t)
	count := readingPages(t, first)
	for _, want := range []string{"Page 1 of ", "## Draft", deepBody, "Role in the reading: declared target", "do not bind you"} {
		if !strings.Contains(first, want) {
			t.Errorf("page 1 should carry %q:\n%s", want, first)
		}
	}
	_, err := x.delegate.AnswerErr(t, x.child, "read", "judge", map[string]any{"findings": []any{}}, "")
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("page 1 of %d is the last served", count)) {
		t.Fatalf("findings before every page was served should be refused, got %v", err)
	}
	serve := x.delegate.Answer(t, x.child, "read", "more", nil, "")
	if !strings.Contains(serve.Instructions, fmt.Sprintf("Page 2 of %d.", count)) || !strings.Contains(serve.Instructions, "## Pre-flight's rules") {
		t.Fatalf("page 2 should carry pre-flight's rules:\n%s", serve.Instructions)
	}
	var later strings.Builder
	for page := 3; page <= count; page++ {
		serve = x.delegate.Answer(t, x.child, "read", "more", nil, "")
		if !strings.Contains(serve.Instructions, fmt.Sprintf("Page %d of %d.", page, count)) {
			t.Fatalf("the next serve should be page %d:\n%s", page, serve.Instructions)
		}
		later.WriteString(serve.Instructions)
	}
	if !strings.Contains(later.String(), "ID: "+captureRef2ID) {
		t.Fatalf("the pages after the rules should carry the briefs:\n%s", later.String())
	}
}

// readingPages returns how many pages the reading page 1 opens takes.
func readingPages(t *testing.T, first string) int {
	t.Helper()
	match := regexp.MustCompile(`Page 1 of (\d+)\.`).FindStringSubmatch(first)
	if match == nil {
		t.Fatalf("page 1 should name the page count:\n%s", first)
	}
	count, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestCaptureDeep_AVerdictReportsItsFindingsList(t *testing.T) {
	world, session := newDeepWorld(t, "deep-findings-list", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	readAllPages(t, x.delegate, x.child)
	_, err := x.delegate.AnswerErr(t, x.child, "read", "judge", nil, "")
	if err == nil || !strings.Contains(err.Error(), "report findings: the whole list") {
		t.Fatalf("a verdict without a findings list should be refused, got %v", err)
	}
}

func TestCaptureDeep_ContestRoundBuildsTheProposal(t *testing.T) {
	world, session := newDeepWorld(t, "deep-contest", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{
		map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes the attachment writer dropping a file.")}},
		map[string]any{"verdict": "the layer is too low", "axis": "ref-fit", "severity": "low", "edits": []any{deepEdit("layer: \"tactical\"", "layer: \"conceptual\"")}},
	})
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	if !strings.Contains(serve.Instructions, "f1 [medium, inconsistent]: the gap names no subject") || strings.Contains(serve.Instructions, "attachment writer") {
		t.Fatalf("the findings should be served for answering, their edits held back:\n%s", serve.Instructions)
	}
	for _, tc := range []struct {
		contest []any
		refusal string
	}{
		{[]any{map[string]any{"target": "f2", "verdict": "contest", "reason": "r"}, map[string]any{"target": "f2", "verdict": "accept"}}, "f2 is answered twice"},
		{[]any{map[string]any{"target": "f2", "verdict": "accept"}}, "a contest names at least one finding with verdict contest"},
		{[]any{map[string]any{"target": "f3", "verdict": "contest", "reason": "r"}}, "f3 names no finding: the verifier raised 2"},
	} {
		_, err := session.AnswerErr(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": tc.contest}, "")
		if err == nil || !strings.Contains(err.Error(), tc.refusal) {
			t.Fatalf("the contest should be refused with %q, got %v", tc.refusal, err)
		}
		session.Refresh(t)
	}
	serve = session.Answer(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{
		map[string]any{"target": "f2", "verdict": "contest", "reason": "the gap is about one command, a tactical matter"},
	}}, "")
	proctest.RequireStep(t, serve, "deepResponding")
	if len(serve.Dispatches) != 1 || serve.Dispatches[0].Session != x.dispatch.Session {
		t.Fatalf("the contest should be handed to the same delegate, got %+v", serve.Dispatches)
	}

	x.delegate.Refresh(t)
	serve = x.delegate.Report(t, x.child, nil)
	proctest.RequireStep(t, serve, "respond")
	if !strings.Contains(serve.Instructions, "- f2: the gap is about one command, a tactical matter") || strings.Contains(serve.Instructions, "- f1:") {
		t.Fatalf("the delegate should be served the contested findings only:\n%s", serve.Instructions)
	}
	_, err := x.delegate.AnswerErr(t, x.child, "respond", "respond", map[string]any{"responses": []any{map[string]any{"target": "f1", "outcome": "held", "note": "n"}}}, "")
	if err == nil || !strings.Contains(err.Error(), "f1 is not contested") {
		t.Fatalf("a response to an uncontested finding should be refused, got %v", err)
	}
	serve = x.delegate.Answer(t, x.child, "respond", "respond", map[string]any{"responses": []any{
		map[string]any{"target": "f2", "outcome": "dropped", "note": "the drafter's reason holds"},
	}}, "")
	proctest.RequireStep(t, serve, "waitAnswers")

	session.Refresh(t)
	serve = session.Report(t, x.instance, nil)
	proctest.RequireStep(t, serve, "deepProposal")
	if !strings.Contains(serve.Instructions, "@@ h1 [f1 applied] line") || strings.Contains(serve.Instructions, "conceptual") || !strings.Contains(serve.Instructions, "after 2 of 3 verifier deliveries") {
		t.Fatalf("the proposal should carry the accepted finding's hunk only, labelled by it:\n%s", serve.Instructions)
	}
	_, err = session.AnswerErr(t, x.instance, "deepProposal", "accept", map[string]any{"body": deepBody + " Something else."}, "")
	if err == nil || !strings.Contains(err.Error(), "the draft differs from the proposal") {
		t.Fatalf("a draft other than the proposal should be refused, got %v", err)
	}
	serve = session.Answer(t, x.instance, "deepProposal", "accept", map[string]any{"body": "A tactical gap: the fixture observes the attachment writer dropping a file."}, "")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, x.instance, "playback", "confirm", nil, "yes, with the verifier's change")
	proctest.RequireStep(t, serve, "verifySummary")
}

// An accept reporting no draft field has sdd write the proposal into the
// draft: the body and every header line its hunks change.
func TestCaptureDeep_AcceptWithoutFieldsWritesTheProposal(t *testing.T) {
	world, session := newDeepWorld(t, "deep-accept-writes", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{
		map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes the attachment writer dropping a file.")}},
		map[string]any{"verdict": "the layer is too low", "axis": "ref-fit", "severity": "low", "edits": []any{deepEdit("layer: \"tactical\"", "layer: \"conceptual\"")}},
	})
	x.pickUp(t)
	serve := session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, serve, "deepProposal")
	if !strings.Contains(serve.Instructions, "@@ h1") || strings.Contains(serve.Instructions, "The draft document it leads to") {
		t.Fatalf("the proposal should serve its hunks only:\n%s", serve.Instructions)
	}
	serve = session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, x.instance, "playback", "confirm", nil, "yes, with the verifier's changes")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, x.instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	entry := proctest.LoadEntry(t, world.GraphDir, entryID)
	if !strings.Contains(entry.Content, "observes the attachment writer dropping a file.") || entry.Layer != model.LayerConceptual {
		t.Fatalf("the entry should carry the proposal's body and layer, got %q on %s", entry.Content, entry.Layer)
	}
}

func TestCaptureDeep_AcceptWithoutFieldsLeavesAStagedBodyToItsFile(t *testing.T) {
	world, session := newDeepWorld(t, "deep-accept-staged", sdd.VerifierPolicy{})
	handle := session.Stage(t, "draft.md", []byte("A tactical gap: the fixture observes something.\n"))
	x := startDeep(t, world, session, stagedBodyDraft(handle))
	x.judge(t, []any{map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes a subject.")}}})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	_, err := session.AnswerErr(t, x.instance, "deepProposal", "accept", nil, "")
	if err == nil || !strings.Contains(err.Error(), `the body comes from the staged file "draft.md"`) {
		t.Fatalf("an accept should leave a staged body to its file, got %v", err)
	}
}

func TestCaptureDeep_AcceptWithoutFieldsRefusesAFieldAcceptCannotWrite(t *testing.T) {
	world, session := newDeepWorld(t, "deep-accept-uncollected", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "the gap lacks a canonical", "axis": "inconsistent", "severity": "low", "edits": []any{deepEdit("layer: \"tactical\"", "layer: \"tactical\"\ncanonical: \"gap\"")}}})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	_, err := session.AnswerErr(t, x.instance, "deepProposal", "accept", nil, "")
	if err == nil || !strings.Contains(err.Error(), "the proposal changes canonical, which accept cannot write") {
		t.Fatalf("a proposal changing a field accept does not carry should be refused by name, got %v", err)
	}
}

func TestCaptureDeep_AProposalWithMarkupCharactersMatchesItsDraft(t *testing.T) {
	world, session := newDeepWorld(t, "deep-markup", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	_, resume := world.Resume(t, x.dispatch.Session, "verifier")
	if page := resume.Open[0].Instructions; !strings.Contains(page, `"desc":"realizes it"`) {
		t.Fatalf("the verifier should read the refs as the drafter reported them:\n%s", page)
	}
	x.delegate.Refresh(t)
	x.judge(t, []any{map[string]any{"verdict": "the ref leaves its condition unsaid", "axis": "ref-fit", "severity": "medium", "edits": []any{
		deepEdit(`"desc":"realizes it"`, `"desc":"realizes it while <limit> & rate > 0"`),
	}}})
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	serve = session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, serve, "deepProposal")
	refs := []any{map[string]any{"id": captureRefID, "kind": "addresses", "desc": "realizes it while <limit> & rate > 0"}}
	serve = session.Answer(t, x.instance, "deepProposal", "accept", map[string]any{"refs": refs}, "")
	proctest.RequireStep(t, serve, "playback")
}

func TestCaptureDeep_OwnerFindingGoesToTheUser(t *testing.T) {
	world, session := newDeepWorld(t, "deep-owner", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "whether this gap is in scope is the owner's call", "axis": "aspiration", "severity": "medium", "owner": true}})
	serve := x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	if !strings.Contains(serve.Instructions, "only the user can settle it") {
		t.Fatalf("an owner finding should be named as the user's:\n%s", serve.Instructions)
	}
	_, err := session.AnswerErr(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{map[string]any{"target": "f1", "verdict": "contest", "reason": "in scope"}}}, "")
	if err == nil || !strings.Contains(err.Error(), "f1 is the user's to settle") {
		t.Fatalf("an owner finding should be refused in a contest, got %v", err)
	}
	session.Refresh(t)
	serve = session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	proctest.RequireStep(t, serve, "deepProposal")
	serve = session.Answer(t, x.instance, "deepProposal", "accept", nil, "")
	proctest.RequireStep(t, serve, "deepUser")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" || !strings.Contains(serve.Instructions, "f1 (only the user can settle it)") {
		t.Fatalf("the open point should go to the user, got %+v:\n%s", serve.PendingChooser, serve.Instructions)
	}
	serve = session.Answer(t, x.instance, "deepUser", "settle", nil, "it is in scope")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestCaptureDeep_ThreeDeliveriesThenTheUser(t *testing.T) {
	world, session := newDeepWorld(t, "deep-cap", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes a subject.")}}})
	x.pickUp(t)
	contest := map[string]any{"deepContest": []any{map[string]any{"target": "f1", "verdict": "contest", "reason": "the subject is in the ref"}}}
	hold := map[string]any{"responses": []any{map[string]any{"target": "f1", "outcome": "held", "note": "a reader of the entry alone cannot tell"}}}
	chooser := "deepAnswer"
	for round := 2; round <= 3; round++ {
		session.Answer(t, x.instance, chooser, "contest", contest, "")
		x.delegate.Refresh(t)
		x.delegate.Report(t, x.child, nil)
		x.delegate.Answer(t, x.child, "respond", "respond", hold, "")
		session.Refresh(t)
		serve := session.Report(t, x.instance, nil)
		proctest.RequireStep(t, serve, "deepProposal")
		chooser = "deepProposal"
	}
	_, err := session.AnswerErr(t, x.instance, "deepProposal", "contest", contest, "")
	if err == nil || !strings.Contains(err.Error(), "made its 3 deliveries") {
		t.Fatalf("a contest after the third delivery should be refused, got %v", err)
	}
	session.Refresh(t)
	serve := session.Answer(t, x.instance, "deepProposal", "escalate", nil, "")
	proctest.RequireStep(t, serve, "deepUser")
	if !strings.Contains(serve.Instructions, "the verifier holds") || !strings.Contains(serve.Instructions, "the drafter contests: the subject is in the ref") {
		t.Fatalf("the user should see both positions of the open point:\n%s", serve.Instructions)
	}
}

func TestCaptureDeep_RefusedRepairIsAVerifierFailure(t *testing.T) {
	world, session := newDeepWorld(t, "deep-failure", sdd.VerifierPolicy{})
	draft := captureDraft()
	draft["confidence"] = "high"
	x := startDeep(t, world, session, draft)
	bad := []any{map[string]any{"verdict": "v", "axis": "contradicts", "severity": "high", "edits": []any{deepEdit("text the draft lacks", "x")}}}
	serve := x.judge(t, bad)
	proctest.RequireStep(t, serve, "repair")
	if !strings.Contains(serve.Instructions, "old text matches the draft 0 times") {
		t.Fatalf("the refusal should name the edit that does not apply:\n%s", serve.Instructions)
	}
	serve = x.delegate.Answer(t, x.child, "repair", "repair", map[string]any{"findings": bad}, "")
	proctest.RequireStatus(t, serve, "completed")
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "deepFailed")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" || !strings.Contains(serve.Instructions, "refused after the repair") {
		t.Fatalf("a verifier failure is the user's to retry or skip, got %+v:\n%s", serve.PendingChooser, serve.Instructions)
	}
	serve = session.Answer(t, x.instance, "deepFailed", "skip", nil, "skip it this once")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, x.instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); entry.Verification != model.VerificationSkipped || entry.Confidence != "low" {
		t.Fatalf("a failure skip writes verification: skipped and confidence low, got %q and %q", entry.Verification, entry.Confidence)
	}
}

func TestCaptureDeep_SkipNeverKeepsTheCaptureWaiting(t *testing.T) {
	world, session := newDeepWorld(t, "deep-never", sdd.VerifierPolicy{Skip: "never"})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	serve = session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	proctest.RequireStep(t, serve, "deepLaunch")
	serve = session.Answer(t, instance, "deepLaunch", "failed", map[string]any{"deepLaunchFailure": "no agent CLI answered"}, "")
	proctest.RequireStep(t, serve, "deepFailed")
	if !strings.Contains(serve.Instructions, "The launch failed: no agent CLI answered") {
		t.Fatalf("the failure should be served in full:\n%s", serve.Instructions)
	}
	_, err := session.AnswerErr(t, instance, "deepFailed", "skip", nil, "skip it")
	if err == nil || !strings.Contains(err.Error(), "verify.skip is never") {
		t.Fatalf("a skip under verify.skip: never should be refused, got %v", err)
	}
	_ = world
}

func TestCaptureDeep_WeighedSkipCapsConfidenceAtMedium(t *testing.T) {
	world, session := newDeepWorld(t, "deep-weighed", sdd.VerifierPolicy{Skip: "weighed"})
	draft := captureDraft()
	draft["confidence"] = "high"
	instance, serve := confirmedDeepCapture(t, session, draft)
	proctest.RequireStep(t, serve, "deepWeigh")
	if serve.PendingChooser == nil || serve.PendingChooser.Kind != "user" || !strings.Contains(serve.Instructions, "Recommended: skip") {
		t.Fatalf("the user weighs the run with sdd's recommendation, got %+v:\n%s", serve.PendingChooser, serve.Instructions)
	}
	serve = session.Answer(t, instance, "deepWeigh", "skip", nil, "not worth it")
	proctest.RequireStep(t, serve, "verifySummary")
	serve = session.Answer(t, instance, "verifySummary", "faithful", map[string]any{"fidelityNote": "matches"}, "")
	entryID, _ := serve.Produced["entryId"].(string)
	if entry := proctest.LoadEntry(t, world.GraphDir, entryID); entry.Verification != model.VerificationSkipped || entry.Confidence != "medium" {
		t.Fatalf("a weighed skip writes verification: skipped and caps confidence at medium, got %q and %q", entry.Verification, entry.Confidence)
	}
}

func TestCaptureDeep_WeighingRecommendsRunForACheckablePath(t *testing.T) {
	checkout := fstest.MapFS{"internal/engine/spec.go": {Data: []byte("package engine")}}
	_, session := newDeepWorld(t, "deep-weigh-run", sdd.VerifierPolicy{Skip: "weighed"}, proctest.WithCheckout(checkout))
	draft := captureDraft()
	draft["body"] = "A tactical gap: `internal/engine/spec.go` refuses the fixture."
	_, serve := confirmedDeepCapture(t, session, draft)
	proctest.RequireStep(t, serve, "deepWeigh")
	if !strings.Contains(serve.Instructions, "Recommended: run, because it names internal/engine/spec.go, a path in the checkout") {
		t.Fatalf("a path found in the checkout should recommend the run:\n%s", serve.Instructions)
	}
}

func TestCaptureDeep_TheVerifierJudgesInThePreflightModelsPlace(t *testing.T) {
	world, session := newDeepWorld(t, "deep-no-preflight-model", sdd.VerifierPolicy{})
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "duplicates", Observation: "the draft repeats a live entry"}}
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	if calls := world.LLM.Calls(llm.PurposePreflight); calls != 0 {
		t.Fatalf("a deep capture should not ask pre-flight's model, got %d calls", calls)
	}
	x := dispatchDeep(t, world, session, instance)
	x.delegate.Refresh(t)
	serve = x.delegate.Answer(t, x.child, "read", "more", nil, "")
	if strings.Contains(serve.Instructions, "store:pageText") {
		t.Fatalf("a reading page should be served whole, not at the store-value cap:\n%s", serve.Instructions)
	}
	for _, want := range []string{"## Pre-flight's rules", "you judge in its place"} {
		if !strings.Contains(serve.Instructions, want) {
			t.Errorf("the verifier should receive the rules pre-flight's model would have judged the draft by, missing %q:\n%s", want, serve.Instructions)
		}
	}
	// The draft reaches the verifier on its reading pages, not again with
	// the rules (20261112-075604-d-tac-t4r).
	if strings.Contains(serve.Instructions, deepBody) {
		t.Errorf("the rules page should leave the draft to the reading pages:\n%s", serve.Instructions)
	}
	serve = x.judge(t, []any{})
	proctest.RequireStep(t, serve, "waitAnswers")
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "verifySummary")
	if calls := world.LLM.Calls(llm.PurposePreflight); calls != 0 {
		t.Fatalf("a verified deep capture should write without pre-flight's model, got %d calls", calls)
	}
}

// The rules pre-flight's model would have applied run past one page; they
// reach the verifier whole across pages, never cut at the page bound
// (20260416-141340-d-prc-8vh).
func TestCaptureDeep_ThePreflightRulesReachTheVerifierWhole(t *testing.T) {
	contract := &model.Entry{
		ID: "20260602-120000-d-prc-cnt", Type: model.TypeDecision, Kind: model.KindContract, Layer: model.LayerProcess,
		Summary: "A contract whose text runs past one page.",
		Content: strings.Repeat("Every capture states the rule this paragraph of the contract carries.\n\n", 1500),
	}
	world, session := newDeepWorld(t, "deep-rules-whole", sdd.VerifierPolicy{}, proctest.WithEntries(contract))
	x := startDeep(t, world, session, captureDraft())
	x.delegate.Refresh(t)
	var rules strings.Builder
	for range 40 {
		serve := x.delegate.Answer(t, x.child, "read", "more", nil, "")
		if strings.Contains(serve.Instructions, "## Pre-flight's rules") {
			rules.WriteString(serve.Instructions)
		}
		if strings.Contains(serve.Instructions, "Every page is served.") {
			break
		}
	}
	for _, want := range []string{"you judge in its place", "Severity on these axes:"} {
		if !strings.Contains(rules.String(), want) {
			t.Errorf("the rules pages should carry the rules whole, missing %q", want)
		}
	}
}

func TestCaptureDeep_ASkippedExchangeFallsBackToThePreflightModel(t *testing.T) {
	world, session := newDeepWorld(t, "deep-skip-fallback", sdd.VerifierPolicy{Skip: "weighed"})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepWeigh")
	world.LLM.PreflightFindings = []proctest.PreflightFinding{{Severity: "high", Category: "duplicates", Observation: "the draft repeats a live entry"}}
	serve = session.Answer(t, instance, "deepWeigh", "skip", nil, "not worth it")
	proctest.RequireStep(t, serve, "reviseOrOverride")
	if calls := world.LLM.Calls(llm.PurposePreflight); calls == 0 {
		t.Fatal("a skipped deep exchange should hand the draft to pre-flight's model before the write")
	}
	if !strings.Contains(serve.Instructions, "the draft repeats a live entry") {
		t.Fatalf("pre-flight's finding should block the write:\n%s", serve.Instructions)
	}
}

func TestCaptureDeep_AutonomousVerifiesBeforePlayback(t *testing.T) {
	world, session := newDeepWorld(t, "deep-autonomous", sdd.VerifierPolicy{}, proctest.WithAutonomousDialogue())
	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	session.Gather(t, instance)
	serve = session.Report(t, instance, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	x := dispatchDeep(t, world, session, instance)
	x.judge(t, []any{})
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "playback")
}

// headlessLauncher stands in for the headless launches: it reports the claude
// CLI, leaves a delegate without a configured launch to the harness, and
// runs each round in the background until the test ends it.
func headlessLauncher(t *testing.T) *test.ScriptedLauncher {
	launcher := test.NewScriptedLauncher(t, "claude")
	launcher.LeaveUnconfigured = true
	return launcher
}

// driveRound drives a launched round's verify delegate the way the CLI would,
// judging the draft clean, and ends the round.
func driveRound(t *testing.T, world *proctest.World, round *test.ScriptedRound) {
	t.Helper()
	delegate := world.Delegate(t, &sdd.WorkflowDispatch{Session: round.Launch.Session})
	readAllPages(t, delegate, round.Launch.Instance)
	delegate.Answer(t, round.Launch.Instance, "read", "judge", map[string]any{"findings": []any{}}, "")
	round.Finish("cli-session", nil)
	// The round records its end in the background; a test ending first would
	// remove the session store under it.
	test.Eventually(t, "recording the round's end", func() bool {
		kept, err := world.App.DelegateLaunches(t.Context(), world.Identity, round.Launch.Session)
		if err == nil && len(kept) >= round.Round.Number {
			return true
		}
		_, _, err = world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: round.Launch.Session, ClientName: "verifier"})
		return err != nil && strings.Contains(err.Error(), "ended")
	})
}

func TestCaptureDeep_NoSubagentFallsBackToAHeadlessLaunch(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-fallback", sdd.VerifierPolicy{}, proctest.WithDelegateLauncher(launcher))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	forked := session.Answer(t, instance, "deepDispatch", "noSubagent", nil, "")
	launches := launcher.Launches()
	if len(launches) != 1 || launches[0].Runner != sdd.DelegateLaunchFallback || launches[0].Project != "proctest" {
		t.Fatalf("a harness without a subagent tool should get the headless fallback, got %+v", launches)
	}
	if dispatch := forked.Dispatches[0]; dispatch.Runner != "claude" || !strings.Contains(dispatch.Instructions, "fell back to the headless claude runner") || dispatch.Session != "" || dispatch.Prompt != "" {
		t.Fatalf("the serve should name the launch it fell back to and hand over no handle, got %+v", dispatch)
	}
	round := launcher.NextRound(t)
	if strings.Contains(round.Round.Prompt, string(session.ID)) {
		t.Fatal("the launched prompt never carries the capture's session handle")
	}
	driveRound(t, world, round)
	session.Refresh(t)
	serve = session.Answer(t, instance, "deepLaunch", "started", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestCaptureDeep_ConfiguredHeadlessLaunchRunsThroughSdd(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-headless", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	forked := session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	if launches := launcher.Launches(); len(launches) != 1 || launches[0].Runner != "codex" {
		t.Fatalf("a configured headless launch should go to sdd's launcher, got %+v", launches)
	}
	dispatch := forked.Dispatches[0]
	if dispatch.Runner != "claude" || !strings.Contains(dispatch.Instructions, "sdd launched the delegate itself: launch nothing") || !strings.Contains(dispatch.Instructions, "works on its own while this move waits") {
		t.Fatalf("the serve should tell the drafter the verifier works on its own, got %+v", dispatch)
	}
	if dispatch.Session != "" || dispatch.Instance != "" || dispatch.Prompt != "" || dispatch.ContinuePrompt != "" || dispatch.Agent != "" {
		t.Fatalf("a verifier sdd launched hands the drafter neither its handles nor a prompt, got %+v", dispatch)
	}
	round := launcher.NextRound(t)
	if round.Round.Number != 1 {
		t.Fatalf("the fork launches the first round, got %+v", round.Round)
	}
	driveRound(t, world, round)
}

func TestCaptureDeep_SubagentLaunchStaysWithTheHarness(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-subagent", sdd.VerifierPolicy{}, proctest.WithDelegateLauncher(launcher))
	x := startDeep(t, world, session, captureDraft())
	if len(launcher.Launches()) != 0 || x.dispatch.Runner != "" || !strings.Contains(x.dispatch.Instructions, "Launch a fresh-context sub-agent") {
		t.Fatalf("a subagent launch is the harness's, got %+v and %d launches", x.dispatch, len(launcher.Launches()))
	}
}

// A change after the verdict whose verifier ended goes to a fresh one, which
// reads the changed passage as a delta.
func TestCaptureDeep_AFreshVerifierOnceTheChildEndedReadsADelta(t *testing.T) {
	world, session := newDeepWorld(t, "deep-delta", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "low", "edits": []any{deepEdit("observes something.", "observes a subject.")}}})
	x.pickUp(t)
	if _, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: x.dispatch.Session}, "the subagent stopped"); err != nil {
		t.Fatal(err)
	}
	session.Refresh(t)
	session.Answer(t, x.instance, "deepAnswer", "accept", nil, "")
	serve := session.Answer(t, x.instance, "deepProposal", "accept", map[string]any{"body": "A tactical gap: the fixture observes a subject."}, "")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, x.instance, "playback", "adjust", map[string]any{"body": "A tactical gap: the fixture observes a subject twice."}, "one more word")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, x.instance, "playback", "confirm", nil, "now write it")
	proctest.RequireStep(t, serve, "deepDispatch")
	again := dispatchDeep(t, world, session, x.instance)
	if !strings.HasPrefix(again.dispatch.Instructions, "A fresh verifier reads the draft anew, as the last one's session ended.") {
		t.Fatalf("the fresh dispatch should say why, got %q", again.dispatch.Instructions)
	}
	_, resume := world.Resume(t, again.dispatch.Session, "verifier")
	if page := resume.Open[0].Instructions; !strings.Contains(page, "## Changed since the last verdict") || !strings.Contains(page, "observes a subject twice.") {
		t.Fatalf("an edit after the verdict should get a delta reading of the changed passage:\n%s", page)
	}
}

func TestCaptureDeep_FailedRoundEndsItsDelegateAndRetries(t *testing.T) {
	launcher := headlessLauncher(t)
	world, session := newDeepWorld(t, "deep-launch-failed", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	round := launcher.NextRound(t)
	failed := round.Launch.Session
	round.Finish("", errors.New("codex failed: exit status 1: not logged in"))
	var err error
	test.Eventually(t, "the failed round reaching the capture", func() bool {
		workflow, refreshErr := world.App.RefreshWorkflow(t.Context(), world.Identity, session.ID)
		if refreshErr != nil {
			t.Fatal(refreshErr)
		}
		_, err = workflow.Advance(t.Context(), world.Identity, sdd.WorkflowAdvanceRequest{Instance: instance})
		return err != nil
	})
	if !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("the capture's next call should fail with the round's error, got %v", err)
	}
	test.Eventually(t, "ending the failed round's delegate", func() bool {
		_, _, resumeErr := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: failed, ClientName: "verifier"})
		return resumeErr != nil && strings.Contains(resumeErr.Error(), "round 1 (claude) failed")
	})
	session.Refresh(t)
	serve = session.Answer(t, instance, "deepLaunch", "failed", map[string]any{"deepLaunchFailure": err.Error()}, "")
	proctest.RequireStep(t, serve, "deepFailed")
	serve = session.Answer(t, instance, "deepFailed", "retry", nil, "try again")
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	driveRound(t, world, launcher.NextRound(t))
	session.Refresh(t)
	serve = session.Answer(t, instance, "deepLaunch", "started", nil, "")
	proctest.RequireStep(t, serve, "verifySummary")
}

func TestCaptureDeep_HeadlessLaunchWithoutALauncherIsRefused(t *testing.T) {
	_, session := newDeepWorld(t, "deep-no-launcher", sdd.VerifierPolicy{Runner: "claude"})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	_, err := session.AnswerErr(t, instance, "deepDispatch", "delegate", nil, "")
	if err == nil || !strings.Contains(err.Error(), "verify.runner claude needs sdd to launch the delegate") {
		t.Fatalf("a headless launch on a server that launches nothing should be refused naming the key, got %v", err)
	}
}

func TestCaptureDeep_ARetriedExchangeReplacesTheWaitingDelegate(t *testing.T) {
	world, session := newDeepWorld(t, "deep-replace", sdd.VerifierPolicy{})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	first := dispatchDeep(t, world, session, instance)
	serve = session.Answer(t, instance, "deepLaunch", "failed", map[string]any{"deepLaunchFailure": "the subagent tool errored"}, "")
	proctest.RequireStep(t, serve, "deepFailed")
	serve = session.Answer(t, instance, "deepFailed", "retry", nil, "try again")
	proctest.RequireStep(t, serve, "deepDispatch")
	second := dispatchDeep(t, world, session, instance)
	if second.dispatch.Session == first.dispatch.Session {
		t.Fatal("a retried exchange should dispatch a new delegate")
	}
	_, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: first.dispatch.Session, ClientName: "verifier"})
	if err == nil || !strings.Contains(err.Error(), "a new deep exchange replaced it") {
		t.Fatalf("the waiting delegate of the earlier exchange should be torn down, got %v", err)
	}
}

func TestCaptureDeep_EveryContestedFindingGetsAResponse(t *testing.T) {
	world, session := newDeepWorld(t, "deep-every-response", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	x.judge(t, []any{
		map[string]any{"verdict": "the gap names no subject", "axis": "inconsistent", "severity": "medium", "edits": []any{deepEdit("observes something.", "observes a subject.")}},
		map[string]any{"verdict": "the layer is too low", "axis": "ref-fit", "severity": "low", "edits": []any{deepEdit("layer: \"tactical\"", "layer: \"conceptual\"")}},
	})
	x.pickUp(t)
	session.Answer(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{
		map[string]any{"target": "f1", "verdict": "contest", "reason": "the subject is named"},
		map[string]any{"target": "f2", "verdict": "contest", "reason": "a tactical matter"},
	}}, "")
	x.delegate.Refresh(t)
	x.delegate.Report(t, x.child, nil)
	_, err := x.delegate.AnswerErr(t, x.child, "respond", "respond", map[string]any{"responses": []any{map[string]any{"target": "f2", "outcome": "conceded", "note": "tactical it is"}}}, "")
	if err == nil || !strings.Contains(err.Error(), "f1 is contested and has no response") {
		t.Fatalf("a contested finding left without a response should be refused, got %v", err)
	}
}

// pollUntilMoved re-serves the capture at step until it has left it.
func pollUntilMoved(t *testing.T, session *proctest.Session, instance, step string) *sdd.WorkflowServe {
	t.Helper()
	var serve *sdd.WorkflowServe
	test.Eventually(t, "the capture leaving "+step, func() bool {
		session.Refresh(t)
		serve = session.Report(t, instance, nil)
		return serve.Step != step
	})
	return serve
}

// A launched round that ends with nothing delivered is a failed round: the
// waiting capture moves to its failure step, naming the round's last message.
func TestCaptureDeep_ARoundEndingWithoutFindingsPutsTheFailureToTheUser(t *testing.T) {
	launcher := headlessLauncher(t)
	_, session := newDeepWorld(t, "deep-empty-round", sdd.VerifierPolicy{Runner: "codex"}, proctest.WithDelegateLauncher(launcher))
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	session.Answer(t, instance, "deepDispatch", "delegate", nil, "")
	round := launcher.NextRound(t)
	serve = session.Answer(t, instance, "deepLaunch", "started", nil, "")
	proctest.RequireStep(t, serve, "deepWait")

	round.FinishSaying("codex-3", "The sdd tools need approval, so I stop.")

	serve = pollUntilMoved(t, session, instance, "deepWait")
	proctest.RequireStep(t, serve, "deepFailed")
	if !strings.Contains(serve.Instructions, "ended without the delegate delivering or concluding") || !strings.Contains(serve.Instructions, "The sdd tools need approval, so I stop.") {
		t.Fatalf("the failure step should name why the round failed:\n%s", serve.Instructions)
	}
	serve = session.Answer(t, instance, "deepFailed", "retry", nil, "try again")
	proctest.RequireStep(t, serve, "deepDispatch")
}

// The capture waiting for the verifier's findings serves one line per check:
// what it waits for and when to check again.
func TestCaptureDeep_TheWaitServesOneLine(t *testing.T) {
	world, session := newDeepWorld(t, "deep-wait-line", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())

	serve := x.pickUp(t)

	proctest.RequireStep(t, serve, "deepWait")
	if !serve.Waiting || strings.Contains(serve.Instructions, "\n") || !strings.Contains(serve.Instructions, "deliver deepFindings") || !strings.Contains(serve.Instructions, "in 20 seconds") {
		t.Fatalf("the wait should serve one line naming the findings and the interval, got %q", serve.Instructions)
	}
}

// A verifier that stops while the capture waits for its findings leaves the
// capture at its failure step, not waiting for a delivery nothing will make.
func TestCaptureDeep_AVerifierStoppingWhileTheCaptureWaitsPutsTheFailureToTheUser(t *testing.T) {
	world, session := newDeepWorld(t, "deep-stopped", sdd.VerifierPolicy{})
	x := startDeep(t, world, session, captureDraft())
	proctest.RequireStep(t, x.pickUp(t), "deepWait")
	if _, err := world.App.AbandonWorkflowSession(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: x.dispatch.Session}, "the subagent stopped"); err != nil {
		t.Fatal(err)
	}

	serve := pollUntilMoved(t, session, x.instance, "deepWait")

	proctest.RequireStep(t, serve, "deepFailed")
	if !strings.Contains(serve.Instructions, "It stopped before delivering: the delegate session was abandoned") {
		t.Fatalf("the failure step should name why the verifier stopped:\n%s", serve.Instructions)
	}
}

// A dispatching answer whose fork failed still stands: started is refused
// until a delegate was dispatched, so the drafter reports the failure.
func TestCaptureDeep_StartedNeedsADispatchedDelegate(t *testing.T) {
	_, session := newDeepWorld(t, "deep-no-fork", sdd.VerifierPolicy{Runner: "claude"})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	if _, err := session.AnswerErr(t, instance, "deepDispatch", "delegate", nil, ""); err == nil {
		t.Fatal("a headless launch without a launcher should fail the dispatching answer")
	}
	session.Refresh(t)
	_, err := session.AnswerErr(t, instance, "deepLaunch", "started", nil, "")
	if err == nil || !strings.Contains(err.Error(), "no verify delegate was dispatched for this exchange") {
		t.Fatalf("started without a dispatched delegate should be refused, got %v", err)
	}
}

// The reading pages hold what they serve in full once the no-repeat rule is
// on, as the MCP surface serves them (20261109-004506-d-cpt-udz, R14): page 1
// hands on the declared target it serves in full, the brief page hands on
// nothing; with the rule off nothing is handed on.
func TestCaptureDeep_ReadingPagesHoldWhatTheyServeInFull(t *testing.T) {
	for _, tc := range []struct {
		name string
		cut  bool
	}{
		{"cuts on", true},
		{"cuts off", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world, session := newDeepWorld(t, "deep-hold", sdd.VerifierPolicy{})
			x := startDeep(t, world, session, captureDraft())
			workflow, _, err := world.App.ResumeWorkflow(t.Context(), world.Identity, sdd.WorkflowResumeRequest{SessionID: x.dispatch.Session, ClientName: "verifier"})
			if err != nil {
				t.Fatal(err)
			}
			if tc.cut {
				workflow.CutRepeats()
			}
			resumed, err := workflow.ServeAll(t.Context(), world.Identity)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(resumed.Open[0].Instructions, "Page 1 of ") {
				t.Fatalf("the resume should serve page 1:\n%s", resumed.Open[0].Instructions)
			}
			count := readingPages(t, resumed.Open[0].Instructions)
			held := workflow.TakeStepFullIDs()
			if slices.Contains(held, captureRefID) != tc.cut || slices.Contains(held, captureRef2ID) {
				t.Fatalf("page 1 should hand on the declared target it serves in full, and only with cuts on, got %v", held)
			}
			delegate := &proctest.Session{World: world, WF: workflow, ID: x.dispatch.Session}
			for page := 2; page <= count; page++ {
				serve := delegate.Answer(t, x.child, "read", "more", nil, "")
				if want := fmt.Sprintf("Page %d of %d.", page, count); !strings.Contains(serve.Instructions, want) {
					t.Fatalf("the next serve should be %q:\n%s", want, serve.Instructions)
				}
				if held := workflow.TakeStepFullIDs(); len(held) != 0 {
					t.Fatalf("the rules page and the brief page should hand on nothing, got %v", held)
				}
			}
		})
	}
}

// A subagent dispatch takes the agent block matching the MCP client the
// session attached through: verify.claude in Claude Code (claude-code),
// verify.codex in Codex (codex-mcp-client), and neither in any other client.
// The dispatch names the model and effort in its fields and its instructions.
func TestCaptureDeep_SubagentDispatchTakesTheClientsAgentBlock(t *testing.T) {
	set := sdd.VerifierPolicy{Claude: sdd.VerifierAgent{Model: "opus", Effort: "high"}, Codex: sdd.VerifierAgent{Model: "gpt-5.5", Effort: "low"}}
	for _, tc := range []struct {
		name, client string
		policy       sdd.VerifierPolicy
		want         sdd.VerifierAgent
		instruction  string
	}{
		{"claude code", "claude-code", set, sdd.VerifierAgent{Model: "opus", Effort: "high"}, "Launch it with the dispatch's model opus and effort high"},
		{"codex", "codex-mcp-client", set, sdd.VerifierAgent{Model: "gpt-5.5", Effort: "low"}, "Launch it with the dispatch's model gpt-5.5 and effort low"},
		{"claude code defaults", "claude-code", sdd.VerifierPolicy{}, sdd.VerifierAgent{Model: "sonnet", Effort: "medium"}, "Launch it with the dispatch's model sonnet and effort medium"},
		{"codex defaults", "codex-mcp-client", sdd.VerifierPolicy{}, sdd.VerifierAgent{Effort: "medium"}, "Launch it with the dispatch's effort medium."},
		{"another client", "cursor", set, sdd.VerifierAgent{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			world, session := newDeepWorld(t, tc.client, tc.policy)
			x := startDeep(t, world, session, captureDraft())
			if x.dispatch.Model != tc.want.Model || x.dispatch.Effort != tc.want.Effort {
				t.Fatalf("the dispatch should carry %+v, got %q and %q", tc.want, x.dispatch.Model, x.dispatch.Effort)
			}
			if tc.instruction == "" && strings.Contains(x.dispatch.Instructions, "Launch it with") {
				t.Fatalf("another client should get no model or effort in the instructions:\n%s", x.dispatch.Instructions)
			}
			if tc.instruction != "" && !strings.Contains(x.dispatch.Instructions, tc.instruction) {
				t.Fatalf("the instructions should name the model and effort to launch with:\n%s", x.dispatch.Instructions)
			}
		})
	}
}

// The dispatch says what pre-flight did: in a deep capture its model never
// judged the draft, and the verifier judges in its place.
func TestCaptureDeep_TheDispatchSaysPreflightsModelDidNotJudge(t *testing.T) {
	_, session := newDeepWorld(t, "deep-dispatch-wording", sdd.VerifierPolicy{})
	_, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	text := test.Untagged(serve.Instructions)
	if !strings.Contains(text, "its model did not judge this draft: the deep verifier judges it now in the model's place") || strings.Contains(text, "Pre-flight left no high finding open") {
		t.Fatalf("the dispatch should say pre-flight's model did not judge the draft:\n%s", text)
	}
}

// A step whose unit never renders pre-flight's rubric serves no notice that
// the rubric was cut for size; the dispatch, which reads it, does.
func TestCaptureDeep_ACutNoticeServesOnlyWhereTheValueRenders(t *testing.T) {
	world, session := newDeepWorld(t, "deep-cut-notice", sdd.VerifierPolicy{})
	instance, serve := confirmedDeepCapture(t, session, captureDraft())
	proctest.RequireStep(t, serve, "deepDispatch")
	const notice = "store:preflightRubric"
	if !strings.Contains(serve.Instructions, notice) {
		t.Fatalf("the dispatch reads the rubric and should name its cut:\n%s", serve.Instructions)
	}
	x := dispatchDeep(t, world, session, instance)
	x.judge(t, []any{map[string]any{"verdict": "the layer is too low", "axis": "ref-fit", "severity": "low"}})
	serve = x.pickUp(t)
	proctest.RequireStep(t, serve, "deepAnswer")
	if strings.Contains(serve.Instructions, notice) {
		t.Fatalf("deepAnswer never renders the rubric and should name no cut of it:\n%s", serve.Instructions)
	}
}

// A finding the verifier withdraws on a contest reaches the drafter with the
// verifier's reason, whole past a store value's cap, to tell the user.
func TestCaptureDeep_AWithdrawnFindingReachesTheDrafter(t *testing.T) {
	note := "the dialogue carries the incidents" + strings.Repeat(", as the user said", 150)
	for _, outcome := range []string{"conceded", "dropped"} {
		t.Run(outcome, func(t *testing.T) {
			world, session := newDeepWorld(t, "deep-withdrawn-"+outcome, sdd.VerifierPolicy{})
			x := startDeep(t, world, session, captureDraft())
			x.judge(t, []any{
				map[string]any{"verdict": "the gap rests on incidents no entry records", "axis": "missing-ref", "severity": "medium"},
				map[string]any{"verdict": "the layer is too low", "axis": "ref-fit", "severity": "low"},
			})
			x.pickUp(t)
			session.Answer(t, x.instance, "deepAnswer", "contest", map[string]any{"deepContest": []any{
				map[string]any{"target": "f1", "verdict": "contest", "reason": "the user named the incidents in the dialogue"},
			}}, "")
			x.delegate.Refresh(t)
			x.delegate.Report(t, x.child, nil)
			x.delegate.Answer(t, x.child, "respond", "respond", map[string]any{"responses": []any{
				map[string]any{"target": "f1", "outcome": outcome, "note": note},
			}}, "")
			session.Refresh(t)
			serve := session.Report(t, x.instance, nil)
			proctest.RequireStep(t, serve, "verifySummary")
			text := test.Untagged(serve.Instructions)
			for _, want := range []string{"- f1 [medium, missing-ref]: the gap rests on incidents no entry records\n  withdrawn: " + note + "\n", "Tell the user which findings the verifier withdrew and why."} {
				if !strings.Contains(text, want) {
					t.Errorf("the drafter should be told of the withdrawn finding, missing %q:\n%s", want, text)
				}
			}
			if strings.Contains(text, "the layer is too low\n  withdrawn") || !strings.Contains(text, "which carried no edit, so the entry was written without a change for them:\n\n- f2 [low, ref-fit]: the layer is too low\n") {
				t.Errorf("a finding the drafter accepted without an edit is named as such, not as withdrawn:\n%s", text)
			}
		})
	}
}

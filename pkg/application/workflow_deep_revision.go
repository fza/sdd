package application

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/textdiff"
)

// A draft changed after a deep verdict goes to the verifier the exchange left
// waiting, as a revision it judges from the reading it holds, so the child
// session lasts across rounds as 20261109-004729-d-cpt-xs5 states. A fresh
// verifier starts only once the child ended, took model.DeepRevisions
// revisions, or ran a headless round that reported no session to resume;
// its dispatch says which.

// Engine-written fields of a revision on the capture's side.
const (
	fieldDeepRevising         = "deepRevising"
	fieldDeepRevisions        = "deepRevisions"
	fieldDeepRevision         = "deepRevision"
	fieldDeepRevisionPages    = "deepRevisionPages"
	fieldDeepRevisionPageFull = "deepRevisionPageFull"
	fieldDeepReadIDs          = "deepReadIDs"
	// fieldDeepFresh is why a fresh verifier starts after a verdict.
	fieldDeepFresh = "deepFresh"
)

// verifyWaitStep is the verify step that waits for answers or a revision.
const verifyWaitStep = "waitAnswers"

// deepRevisable reports whether the capture's draft change can go to its
// live verifier as a revision: an exchange reached a verdict, the verifier
// waits for its next input in a child its runner continues, the harness's
// subagent or a headless round that reported a session to resume, and it
// has revisions left. Otherwise, past a verdict, it names in a few words why
// a fresh verifier starts.
func (w *WorkflowSession) deepRevisable(ctx *engine.Context) (bool, string, error) {
	verdict, ok := readDeepVerdict(ctx.Store)
	if !ok || verdict.Outcome == "skipped" {
		return false, "", nil
	}
	taken, _ := ctx.Store.Get(fieldDeepRevisions)
	if n := deepRoundCount(taken); n >= model.DeepRevisions {
		return false, fmt.Sprintf("the last one judged %d revisions, its limit", n), nil
	}
	runner, err := w.delegateRunner(ctx.Instance, verifyProcedure)
	if err != nil {
		return false, "", err
	}
	for _, live := range w.session.LiveForks() {
		if live.Instance != ctx.Instance || live.Fork.Procedure != verifyProcedure {
			continue
		}
		waits, err := w.delegateWaits(live.Fork.Child)
		if err != nil || !waits {
			return false, "the last one no longer waits for a revision", err
		}
		if runner == "" {
			return true, "", nil
		}
		resumable, err := w.delegateResumable(live.Fork.Child)
		if err != nil || !resumable {
			return false, "its runner reported no session to resume", err
		}
		return true, "", nil
	}
	return false, "the last one's session ended", nil
}

// delegateResumable reports whether the child's last launched round reported
// a session its next round resumes.
func (w *WorkflowSession) delegateResumable(child engine.Link) (bool, error) {
	_, _, stored, err := w.app.resolveSession(w.ctx, w.identity, SessionID(child.Session))
	if err != nil {
		return false, err
	}
	records, err := delegateLaunches(stored.Events)
	if err != nil {
		return false, err
	}
	n := len(records)
	return n > 0 && records[n-1].Session != "", nil
}

// delegateWaits reports whether the child's task waits for the capture's
// next input; an ended child waits for nothing.
func (w *WorkflowSession) delegateWaits(child engine.Link) (bool, error) {
	held := w.held[SessionID(child.Session)]
	if held == nil {
		linked, refusal, err := w.app.loadLinked(w.ctx, w.identity, SessionID(child.Session))
		if err != nil || refusal != "" {
			return false, err
		}
		held = linked
	}
	inst, ok := held.session.Instance(child.Instance)
	return ok && inst.Status == engine.StatusRunning && inst.Step == verifyWaitStep, nil
}

// deepRevisionPages renders a revision's reading: page 1 the revised draft
// and its changed passages, later pages the entries read since the verifier
// last read, which held names.
func (w *WorkflowSession) deepRevisionPages(ctx *engine.Context, base string, number int, held deepHeld) []model.DeepReadingPage {
	var lead strings.Builder
	fmt.Fprintf(&lead, "## Revision %d of the draft\n\nThe draft changed after your last findings, and you hold the reading the earlier pages served. Judge the changed passages below against that reading and against the draft as a whole, then report **judge** with your whole findings list over this draft document: your earlier findings and the drafter's answers to them are over. Any later page holds the entries read since your reading. If you do not hold the earlier reading, report **reread** first.\n\n", number)
	fmt.Fprintf(&lead, "## Draft\n\n```\n%s\n```\n", base)
	lead.WriteString(preflightRaised(ctx.Store))
	lead.WriteString("\n## Changed since your last findings\n\n" + revisionPassages(ctx.Store, base))
	pages, _ := w.deepReading(ctx, lead.String(), held)
	return pages
}

// revisionPassages renders the passages of base that changed since the
// verdict.
func revisionPassages(store *engine.Store, base string) string {
	previous, _ := readDeepVerdict(store)
	changed := textdiff.ChangedPassages(previous.Document, base)
	if len(changed) == 0 {
		return "No passage of the draft changed: its staged attachments or its target did.\n"
	}
	var b strings.Builder
	for _, passage := range changed {
		b.WriteString("```\n" + passage + "\n```\n")
	}
	return b.String()
}

func (w *WorkflowSession) sendDeepRevision(ctx *engine.Context) error {
	base, _ := workflowStoreString(ctx.Store, fieldDeepBase)
	number, _ := ctx.Store.Get(fieldDeepRevisions)
	return ctx.Store.WriteEngine(fieldDeepRevision, fmt.Sprintf("Revision %d of the draft. Changed since your last findings:\n\n%s", deepRoundCount(number), revisionPassages(ctx.Store, base)))
}

// loadVerifyRevision loads the revised draft and the revision's reading from
// the capture into the verify task, serves its page 1, and opens a fresh
// exchange over the revised draft.
func (w *WorkflowSession) loadVerifyRevision(ctx *engine.Context) error {
	parent, err := w.dispatchingStore()
	if err != nil {
		return err
	}
	base, _ := workflowStoreString(parent, fieldDeepBase)
	pages := workflowStoreStrings(parent, fieldDeepRevisionPages)
	if base == "" || len(pages) == 0 {
		return errors.New("the dispatching capture holds no revision")
	}
	full, err := storeDeep[[]string](parent, fieldDeepRevisionPageFull)
	if err != nil {
		return err
	}
	writes := map[string]any{fieldVerifyBase: base, fieldVerifyPages: pages, fieldVerifyPageFull: full, fieldVerifyRepair: nil, fieldVerifyResponded: nil}
	for _, name := range slices.Sorted(maps.Keys(writes)) {
		err := ctx.Store.WriteEngine(name, writes[name])
		if err != nil {
			return err
		}
	}
	return w.showVerifyPage(ctx.Store, 0)
}

package application

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// pageWait is how long a later page of a check waits for the scores its
// held lines still miss (20261121-223938-d-tac-rqi).
const pageWait = 3 * time.Second

// heldCheck is what a capture's graph check keeps for its later pages: the
// lines not shown yet in their order, which of them are title lines, how
// many it counts without listing, the lines it marked so far and the page
// served last.
type heldCheck struct {
	parts   *checkParts
	held    []model.Line
	titles  map[string]bool
	counted int
	marked  int
	page    int
	key     rankingKey
}

// firstPage cuts a check's result too large for one page to the page that
// fits budget: every marked line and as many lines in doubt and by title as
// fit. The rest stay held for later pages, which the scores that arrive
// meanwhile place; a judge that ran keeps scoring them in the background.
// Only the lines served count as shown.
func (w *WorkflowSession) firstPage(instance string, result GatherResult, budget int) (GatherResult, error) {
	parts := result.check
	lines := parts.served.Lines
	pool := slices.Concat(lines.Doubt, lines.Titles)
	titles := map[string]bool{}
	for _, line := range lines.Titles {
		titles[line.Entry.ID] = true
	}
	render := func(n int) (string, error) {
		served := *parts.served
		served.Lines = model.Lines{Bearing: lines.Bearing, Judged: lines.Judged}
		for _, line := range pool[:n] {
			if titles[line.Entry.ID] {
				served.Lines.Titles = append(served.Lines.Titles, line)
			} else {
				served.Lines.Doubt = append(served.Lines.Doubt, line)
			}
		}
		var rendered bytes.Buffer
		err := presenters.RenderGather(&rendered, &served)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(rendered.String(), "\n") + "\n\n" + pageTrailer(instance, 2), nil
	}
	n, text, err := fitPage(len(pool), budget, render)
	if err != nil {
		return GatherResult{}, err
	}
	held := &heldCheck{parts: parts, held: slices.Clone(pool[n:]), titles: titles, counted: len(lines.Counted), marked: len(lines.Bearing), page: 1}
	shown := lineIDsOf(slices.Concat(lines.Bearing, pool[:n]))
	result.Text, result.SummaryIDs = text, shown
	result.payload.Shown = slices.Clone(shown)
	if parts.judging && parts.judge.Scorer != nil {
		held.key = rankingKey{session: w.session.ID, instance: instance, judge: parts.judge.Judge, subject: model.RelevanceSubjectHash(parts.subject)}
		w.scoreHeld(held)
	}
	if w.checks == nil {
		w.checks = map[string]*heldCheck{}
	}
	w.checks[instance] = held
	return result, nil
}

// scoreHeld has the check's judge score its unscored held lines in the
// background, within the ranking time limit.
func (w *WorkflowSession) scoreHeld(held *heldCheck) {
	parts := held.parts
	var pending []relevance.Candidate
	for _, line := range held.held {
		if line.Score == nil {
			e := line.Entry
			pending = append(pending, entryCandidate(parts.served.Graph, e))
		}
	}
	if len(pending) == 0 {
		return
	}
	run := parts.runtime.rankings.begin(held.key, parts.judge, parts.subject, pending, parts.runtime.rankingTimeout())
	if run == nil {
		return
	}
	ctx, release := w.app.background.detached(w.ctx)
	started := w.app.background.start(func() {
		defer release()
		run.score(ctx)
	})
	if !started {
		release()
		run.abandon()
	}
}

// checkPage builds page of the capture's last check from its held lines and
// every score that arrived by then, waiting pageWait for the ones missing: a
// line a late score marks comes under its own heading, one a late score sets
// aside is counted, and the rest serve in order as far as budget holds. The
// page's lines, its marks and its late scores are credited to the capture.
func (w *WorkflowSession) checkPage(ctx context.Context, instance string, page, budget int) (GatherResult, error) {
	held := w.checks[instance]
	switch {
	case held == nil:
		return GatherResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf("sdd: no graph check of %s holds more lines: call gather with instance %s and without page", instance, instance)}
	case page != held.page+1:
		return GatherResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf("sdd: the next page of %s's graph check is page %d", instance, held.page+1)}
	}
	parts := held.parts
	var scores map[string]uint8
	if held.key != (rankingKey{}) {
		scores = parts.runtime.rankings.peek(ctx, held.key, pageWait)
	}
	late := model.ScoreHeld(held.held, scores, parts.thresholds, held.marked)
	render := func(n int) (string, error) {
		checkPage := presenters.CheckPage{Number: page, Late: late.Bearing, SetAside: len(late.SetAside)}
		for _, line := range late.Held[:n] {
			if held.titles[line.Entry.ID] {
				checkPage.Titles = append(checkPage.Titles, line)
			} else {
				checkPage.Doubt = append(checkPage.Doubt, line)
			}
		}
		if n == len(late.Held) {
			checkPage.Counted = held.counted
		}
		var rendered bytes.Buffer
		presenters.RenderCheckPage(&rendered, parts.served.Graph, checkPage, w.repeats())
		text := strings.TrimRight(rendered.String(), "\n")
		if n < len(late.Held) {
			return text + "\n\n" + pageTrailer(instance, page+1), nil
		}
		return text + "\n\n(the last page of this graph check)", nil
	}
	n, text, err := fitPage(len(late.Held), budget, render)
	if err != nil {
		return GatherResult{}, err
	}
	shown := lineIDsOf(slices.Concat(late.Bearing, late.Held[:n]))
	fresh := map[string]uint8{}
	for _, line := range slices.Concat(late.Bearing, late.Held) {
		if score, ok := scores[line.Entry.ID]; ok {
			fresh[line.Entry.ID] = score
		}
	}
	for _, id := range late.SetAside {
		fresh[id] = scores[id]
	}
	payload := model.ReadPayload{Shown: shown, Marked: lineIDsOf(late.Bearing)}
	if len(fresh) > 0 {
		payload.Prefilter = &model.PrefilterPayload{Judge: parts.judge.Judge, Forward: parts.judge.Scorer != nil && parts.judge.Scorer.Direction() == relevance.Forward, Subject: model.RelevanceSubjectHash(parts.subject), Scores: fresh, SetAside: late.SetAside}
	}
	err = w.session.Record(engine.Read{Tool: "gather", Instance: instance, Summary: shown, Grounding: payload})
	if err != nil {
		return GatherResult{}, err
	}
	held.held, held.marked, held.page = slices.Clone(late.Held[n:]), held.marked+len(late.Bearing), page
	if len(held.held) == 0 {
		delete(w.checks, instance)
	}
	return GatherResult{Project: parts.runtime.options.Project, Text: text, SummaryIDs: shown, payload: payload}, nil
}

// fitPage finds the most of total lines a page holds within budget, at
// least none, and the page's text; render builds the page with the first n.
func fitPage(total, budget int, render func(n int) (string, error)) (int, string, error) {
	low, high := 0, total
	for low < high {
		mid := (low + high + 1) / 2
		text, err := render(mid)
		if err != nil {
			return 0, "", err
		}
		if model.ServedChars(text) <= budget {
			low = mid
		} else {
			high = mid - 1
		}
	}
	text, err := render(low)
	return low, text, err
}

func pageTrailer(instance string, next int) string {
	return fmt.Sprintf("(more lines held: call gather with instance %s and page %d; it builds the page from the scores that arrived by then)", instance, next)
}

func lineIDsOf(lines []model.Line) []string {
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, line.Entry.ID)
	}
	return ids
}

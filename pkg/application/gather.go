package application

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// GroundingPolicy is a project's grounding configuration. A zero limit takes
// its default.
type GroundingPolicy struct {
	Enforce       bool
	DepthDown     int
	DepthUp       int
	PrefilterFrom int
	// Prefilter switches the prefilter on where the runtime has one.
	Prefilter bool
	// AutoCalibrate lets a server score the ranking judge's unscored
	// labelled pairs in the background (Application.AutoCalibrate).
	AutoCalibrate bool
	// JudgeTimeout bounds the ranking judge of a graph check; zero takes
	// model.DefaultJudgeTimeout, model.NoJudgeTimeout runs it to the end.
	// JudgeMinimum is the count it scores before the check goes on without
	// it; zero takes model.DefaultJudgeMinimum.
	JudgeTimeout time.Duration
	JudgeMinimum int
	// RecheckResponsible names the participants catch-up shows the recheck
	// list to; empty shows it to everyone.
	RecheckResponsible []string
}

func (p GroundingPolicy) resolved() GroundingPolicy {
	defaults := model.VerifyConfig{}.GroundingPolicy()
	for _, field := range []struct {
		value    *int
		fallback int
	}{{&p.DepthDown, defaults.DepthDown}, {&p.DepthUp, defaults.DepthUp}, {&p.PrefilterFrom, defaults.PrefilterFrom}, {&p.JudgeMinimum, defaults.JudgeMinimum}} {
		if *field.value == 0 {
			*field.value = field.fallback
		}
	}
	if p.JudgeTimeout == 0 {
		p.JudgeTimeout = defaults.JudgeTimeout
	}
	return p
}

// gatherSearchLimit is how many hits one grounding search lists. Listings are
// never bounded: a topic's live population is the read.
const gatherSearchLimit = 20

// GatherRequest asks for a grounding serve. Draft, Kind and Topics describe
// the draft; IDs opens entries in full; Query and Terms are the agent's
// searches, run beside the search with the draft's own text.
type GatherRequest struct {
	SyncMode          SearchSyncMode
	Branch            string
	BranchFromSession bool
	Draft             string
	Kind              string
	Topics            []string
	IDs               []string
	Query             string
	Terms             []string
	// Answered names entries the capture already answered (its refs and
	// verdicts), which gather sets aside instead of asking again.
	Answered []string
	// References names the entries the draft references (refs, supersedes,
	// closes): what the far floor keeps its far entries away from
	// (model.FarFloor). Without them a Subject's own references stand in.
	References []string
	// Subject names an existing entry the reading grounds: its body, kind
	// and topics stand in for an empty draft, and the relevance ledger's
	// stored verdicts against it answer repeat pairs without a new judgment.
	Subject string
	// Wait has the judge score until the minimum holds, without a time
	// limit: the choice a person makes after a check judged too few.
	Wait bool
	// Rejudge has the judge score every candidate against the draft now, the
	// judgements the capture keeps included (20261121-233807-d-tac-4iw).
	Rejudge bool
	// Held reports whether the consumer already holds an entry in full; such
	// a candidate prints as a pointer. Nil serves every line in full,
	// as the CLI does. Served records a body served in full.
	Held   func(id string) bool
	Served func(id, body string)
	// since is the corpus mark of the capture's reading; the live entries it
	// lacks are listed as landed since the reading began.
	since *model.CorpusMark
	// reading is the capture's reading, whose prefilter scores a later gather
	// reuses instead of scoring again.
	reading *model.Reading
	// client is the session's MCP client, which picks the ranking judge
	// (rankingJudge); empty for a reading without one.
	client string
	// openOnly opens the IDs alone in a capture whose draft fills the
	// request: no search, listing or judge call.
	openOnly bool
	// judgeLanded has an open-only call judge and mark the landed entries it
	// lists (WorkflowSession.listLanded).
	judgeLanded bool
	// unattended marks a session without a person at the check
	// (model.DialogueMode.AgentAnswers).
	unattended bool
	// alsoListed are topics the agent asked a capture's check to list beside
	// the draft's own, which alone describe the draft.
	alsoListed []string
}

// GatherResult is the rendered serve and the entries it served.
type GatherResult struct {
	Project    ProjectRef
	Text       string
	FullIDs    []string
	SummaryIDs []string
	payload    model.ReadPayload
	check      *checkParts
}

// checkParts are what a capture's check builds its later pages from: the
// result its text renders, and the judge, the draft and the thresholds it
// placed its lines by; judging marks a judge that ran.
type checkParts struct {
	served     *query.GatherResult
	runtime    *ProjectRuntime
	judge      Prefilter
	judging    bool
	subject    string
	thresholds model.RelevanceThresholds
}

// Gather serves the reading of a draft: the semantic search with the draft's
// text, the agent's searches and the live topic listings, each run once on
// one snapshot, or the entries named by ID alone. It credits nothing; a
// capture is credited through WorkflowSession.Gather.
func (a *Application) Gather(ctx context.Context, identity RequestIdentity, project ProjectID, request GatherRequest) (result GatherResult, err error) {
	if !request.SyncMode.Valid() {
		return GatherResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: search synchronization mode is required: none, local, or all"}
	}
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return GatherResult{}, err
	}
	selected, err := acquireSnapshotForSearch(ctx, runtime, request.Branch, "")
	if err != nil {
		return GatherResult{}, withSessionBindingTargetError(request.Branch, request.BranchFromSession, err)
	}
	defer selected.releaseInto(&err)
	return a.gatherOn(ctx, selected.runtime, selected, request)
}

func (a *Application) gatherOn(ctx context.Context, runtime *ProjectRuntime, selected *readSnapshotSelection, request GatherRequest) (GatherResult, error) {
	snapshot := selected.snapshot
	graph := snapshot.graph
	policy := runtime.options.Grounding.resolved()
	live := model.GroundingPopulation(graph)
	liveIDs := make([]string, len(live))
	for i, e := range live {
		liveIDs[i] = e.ID
	}

	opened, err := graph.ResolveIDs(request.IDs)
	if err != nil {
		return GatherResult{}, err
	}
	opened = uniqueIDs(opened)
	for _, id := range opened {
		if graph.ByID[id] == nil {
			return GatherResult{}, fmt.Errorf("sdd: gather: entry %s does not resolve", id)
		}
	}
	openOnly := request.openOnly || request.reading == nil && len(opened) > 0 && strings.TrimSpace(request.Draft) == "" && request.Query == "" &&
		len(request.Terms) == 0 && len(request.Topics) == 0 && request.Subject == ""

	if request.Subject != "" {
		resolved, err := graph.ResolveIDs([]string{request.Subject})
		if err != nil {
			return GatherResult{}, err
		}
		subject := graph.ByID[resolved[0]]
		if subject == nil {
			return GatherResult{}, fmt.Errorf("sdd: gather: subject %s does not resolve", request.Subject)
		}
		request.Subject = subject.ID
		if len(request.References) == 0 {
			request.References = slices.Concat(model.RefIDs(subject.Refs), subject.Supersedes, subject.Closes)
		}
		if strings.TrimSpace(request.Draft) == "" {
			request.Draft, request.Kind = subject.Content, string(subject.Kind)
			if len(request.Topics) == 0 {
				for _, topic := range graph.EffectiveTopics(subject) {
					request.Topics = append(request.Topics, topic.String())
				}
			}
		}
	}

	var reads []model.GroundingRead
	switch {
	case openOnly:
	case strings.TrimSpace(request.Draft) != "" || request.Query != "" || len(request.Terms) > 0:
		candidate := make(map[string]bool, len(live))
		for _, e := range live {
			candidate[e.ID] = true
		}
		searched, err := a.gatherSearches(ctx, runtime, selected, request, candidate)
		if err != nil {
			return GatherResult{}, err
		}
		reads = append(reads, searched...)
		for _, label := range uniqueIDs(slices.Concat(request.Topics, request.alsoListed)) {
			path, err := model.ParseTopicPath(label)
			if err != nil {
				return GatherResult{}, fmt.Errorf("sdd: gather: topic %q: %w", label, err)
			}
			listed := model.TopicListing(graph, live, path)
			ids := make([]string, len(listed))
			for i, e := range listed {
				ids[i] = e.ID
			}
			reads = append(reads, model.GroundingRead{Mode: model.GroundingListing, Text: path.String(), IDs: ids})
		}
	case len(opened) == 0:
		return GatherResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: gather needs the draft, a search or ids to open: the reading searches from the draft's text"}
	}
	landed := request.since.Since(liveIDs)
	if openOnly {
		landed = slices.DeleteFunc(landed, request.reading.IsListed)
	}
	if len(landed) > 0 {
		reads = append(reads, model.GroundingRead{Mode: model.GroundingNew, IDs: landed})
	}
	if len(opened) > 0 {
		reads = append(reads, model.GroundingRead{Mode: model.GroundingOpened, IDs: opened})
	}

	candidates, setAside := model.GatherCandidates(graph, live, reads, opened, request.Answered)
	candidates = slices.DeleteFunc(candidates, func(c model.GatherCandidate) bool { return c.Entry.ID == request.Subject })
	var similarity map[string]float64
	if text := cmp.Or(strings.TrimSpace(request.Draft), request.Query); text != "" {
		similarity, err = runtime.draftSimilarity(ctx, text, model.Unsearched(candidates, reads))
		if err != nil {
			return GatherResult{}, fmt.Errorf("sdd: gather: similarity to the draft: %w", err)
		}
	}
	candidates = model.OrderGatherCandidates(graph, candidates, reads, similarity, request.References)
	prefilter := runtime.rankingJudge(request.client)
	judge := prefilter.Judge
	ledger, _, hiding, err := runtime.judgeHiding(ctx, prefilter, len(live))
	if err != nil {
		return GatherResult{}, err
	}
	// A rule hides only as far as its step allows (model.HidingState).
	thresholds := model.SteppedThresholds(hiding.thresholds, hiding.threshold.Step)
	split := model.SplitByLedger(candidates, request.Subject, ledger, judge, thresholds)
	if len(split.NotRelevant) > 0 {
		setAside = append(setAside, model.GatherSetAside{Reason: model.SetAsideJudged, IDs: split.NotRelevant})
	}
	labels := ledger.Labels(judge)
	var held map[string]uint8
	var due []string
	var draftMark *model.DraftMark
	var floor *model.FarFloor
	below := map[string]bool{}
	subject := ""
	if strings.TrimSpace(request.Draft) != "" {
		subject = model.RelevanceText(request.Kind, request.Topics, "", request.Draft)
		key := model.RelevanceSubjectHash(subject)
		held = request.reading.Kept(judge)
		if !openOnly {
			held, due, draftMark = keptJudgements(graph, live, request, judge, thresholds, candidates, reads)
		}
		// A floor at compute-only sets nothing aside, verdicts held from before included.
		if prefilter.Scorer != nil {
			if hiding.floor.Step != model.StepComputeOnly {
				below = model.StandingBelowFloor(graph, request.References, split.Undecided, request.reading.BelowFloor(judge, key))
			}
			listed := slices.Concat(request.Answered, request.References, opened, []string{request.Subject})
			for _, read := range reads {
				listed = append(listed, read.IDs...)
			}
			floor = model.NewFarFloor(graph, live, model.FarDraft{Subject: key, Topics: request.Topics, References: request.References}, listed)
			if floor != nil {
				floor.Form = model.FloorForm(hiding.floor.Step)
			}
		}
	}

	pass := prefilterPass{status: model.PrefilterOff, note: "ids only: no search, listing or judge call", scores: held}
	judged := !openOnly || request.judgeLanded
	if judged {
		budget := judgeBudget{limit: policy.JudgeTimeout, target: model.JudgeTarget(len(split.Undecided), policy.JudgeMinimum), wait: request.Wait, unattended: request.unattended}
		endCheck := a.liveChecks.begin()
		pass, err = runPrefilter(ctx, prefilter, policy, budget, subject, graph, split.Undecided, held, floor)
		endCheck()
		if err != nil {
			return GatherResult{}, err
		}
		// A second look the judge did not give leaves the kept judgement.
		kept := request.reading.Kept(judge)
		for _, id := range due {
			if _, scored := pass.scores[id]; !scored {
				if score, ok := kept[id]; ok {
					pass.scores[id] = score
				}
			}
		}
	}
	var shortfall *model.JudgeShortfall
	if pass.judged {
		state, err := runtime.judgeState(ctx, ledger, prefilter, graph, pass.broken)
		if err != nil {
			return GatherResult{}, err
		}
		limit := policy.JudgeTimeout
		if limit <= 0 || request.Wait {
			limit = pass.waited
		}
		if short, ok := model.NewJudgeShortfall(pass.scored, len(split.Undecided)-pass.unread, policy.JudgeMinimum, limit, state); ok {
			short.Unattended = request.unattended
			shortfall = &short
			slogutils.FromContext(ctx).Warn("gather: the ranking judge scored fewer candidates than the minimum",
				"judge", judge, "judged", short.Judged, "listed", short.Listed, "minimum", short.Minimum, "cause", short.Cause, "waited", short.Waited, "unattended", short.Unattended)
		}
	}
	floorWould := pass.wouldBelow
	if hiding.floor.Step == model.StepComputeOnly {
		pass.below = nil
	}
	for _, id := range pass.below {
		below[id] = true
	}
	marked := map[string]string{}
	for _, candidate := range split.Relevant {
		marked[candidate.Entry.ID] = ""
	}
	// A graph check shows no set-aside entry blind (20261121-210755-d-tac-d5e): no seed.
	lines := model.PlaceLines(model.LineInput{
		Candidates: slices.Concat(split.Relevant, split.Undecided),
		Scores:     pass.scores, BelowFloor: below, Thresholds: thresholds,
		Widened:    model.JudgeRankingHealth(labels).Weak(),
		SelfJudged: judged && subject != "" && (pass.status == model.PrefilterOff || pass.status == model.PrefilterFailed || shortfall != nil),
		Hops:       model.RefHops(graph, request.References), Marked: marked,
	})
	marks := []model.RuleMark{
		{Rule: model.RuleThreshold, Step: hiding.threshold.Step, Would: model.WouldSetAside(split.Undecided, pass.scores, below, hiding.thresholds)},
		{Rule: model.RuleFarFloor, Step: hiding.floor.Step, Would: floorWould},
	}
	setAside = append(setAside, lines.SetAside...)
	var filtered []string
	floored := 0
	for _, group := range lines.SetAside {
		switch group.Reason {
		case model.SetAsideFloor:
			floored = len(group.IDs)
		case model.SetAsideFiltered:
			filtered = group.IDs
		}
	}
	if pass.floored || floored > 0 {
		clause := fmt.Sprintf("the far floor set aside %d", floored)
		if pass.note != "" {
			clause = "; " + clause
		}
		pass.note += clause
	}
	for _, mark := range marks {
		if mark.Step == model.StepComputeOnly && mark.Would > 0 {
			pass.note += fmt.Sprintf("; the %s holds no step to hide at and would set aside %d", mark.Rule, mark.Would)
		}
	}
	served := &query.GatherResult{
		Graph: graph, Reads: reads, Corpus: model.NewCorpusMark(liveIDs), Prefilter: pass.status, PrefilterNote: pass.note, Shortfall: shortfall,
		Found: len(candidates), Judged: lines.Judged + len(split.NotRelevant), Lines: lines, SetAside: setAside,
		NoMeaningSearch: !openOnly && !runtime.searchesByMeaning(),
	}
	for _, candidate := range split.Relevant {
		if _, scored := pass.scores[candidate.Entry.ID]; !scored {
			served.Judged++
		}
	}
	if request.Held != nil {
		served.Held, served.Served = request.Held, request.Served
	}
	if len(opened) > 0 {
		shown, err := snapshot.finder.Show(query.ShowQuery{IDs: opened})
		if err != nil {
			return GatherResult{}, err
		}
		served.Opened = shown
	}
	result := GatherResult{Project: runtime.options.Project, FullIDs: slices.Clone(opened), SummaryIDs: lines.Shown()}
	result.check = &checkParts{served: served, runtime: runtime, judge: prefilter, judging: pass.judged && policy.Prefilter, subject: subject, thresholds: thresholds}
	var rendered bytes.Buffer
	err = presenters.RenderGather(&rendered, served)
	if err != nil {
		return GatherResult{}, err
	}
	result.Text = strings.TrimRight(rendered.String(), "\n")
	result.payload = model.ReadPayload{Reads: reads, Full: slices.Clone(opened), Shown: slices.Clone(result.SummaryIDs), Marked: lines.MarkedIDs(), Corpus: served.Corpus, Shortfall: shortfall, Draft: draftMark}
	if len(pass.fresh) > 0 || len(filtered) > 0 {
		var cut uint8
		if hiding.thresholds.Valid {
			cut = model.ScoreByte(hiding.thresholds.SetAside)
		}
		result.payload.Prefilter = &model.PrefilterPayload{Judge: judge, Forward: prefilter.Scorer != nil && prefilter.Scorer.Direction() == relevance.Forward, Subject: model.RelevanceSubjectHash(subject), Scores: pass.fresh, Below: pass.below, SetAside: filtered, Cut: cut, Hiding: marks}
	}
	return result, nil
}

// keptJudgements returns the judgements a check reuses, those due a second
// look (model.SecondLook) and the draft the check reads against. A rejudge
// reuses none.
func keptJudgements(graph *model.Graph, live []*model.Entry, request GatherRequest, judge string, thresholds model.RelevanceThresholds, ordered []model.GatherCandidate, reads []model.GroundingRead) (map[string]uint8, []string, *model.DraftMark) {
	mark := &model.DraftMark{Topics: slices.Clone(request.Topics), References: slices.Clone(request.References)}
	for _, read := range reads {
		if read.Mode == model.GroundingSemantic && read.Text == "" {
			mark.Nearest = slices.Clone(read.IDs[:min(model.SecondLookTop, len(read.IDs))])
		}
	}
	judged := request.reading.JudgedDraft()
	moved := model.DraftMoved(judged, mark.Nearest)
	mark.Judged = judged == nil || moved
	kept := request.reading.Kept(judge)
	if request.Rejudge {
		return nil, slices.Sorted(maps.Keys(kept)), mark
	}
	order := make([]string, len(ordered))
	for i, candidate := range ordered {
		order[i] = candidate.Entry.ID
	}
	stale := request.reading.Stale(judge, model.RelevanceSubjectHash(model.RelevanceText(request.Kind, request.Topics, "", request.Draft)))
	due := model.SecondLook{
		Graph: graph, Live: live, Kept: stale, Thresholds: thresholds, Order: order,
		Previous: request.reading.LastDraft(), Current: *mark, Moved: moved,
	}.Due()
	held := maps.Clone(kept)
	for _, id := range due {
		delete(held, id)
	}
	return held, due, mark
}

// gatherSearches runs the semantic search with the draft's own text, the
// agent's semantic search and its keyword searches. Each search lists up to
// gatherSearchLimit hits among candidate, the live entries that can become
// candidates, so closed entries and records of who takes part never take a
// slot.
func (a *Application) gatherSearches(ctx context.Context, runtime *ProjectRuntime, selected *readSnapshotSelection, request GatherRequest, candidate map[string]bool) ([]model.GroundingRead, error) {
	draft := strings.TrimSpace(request.Draft)
	phrase := request.Query
	if !runtime.searchesByMeaning() {
		draft, phrase = "", ""
	} else if draft != "" || phrase != "" {
		if err := checkEmbedder(ctx, runtime); err != nil {
			return nil, err
		}
	}
	if draft != "" {
		if err := secrets.CheckText("the draft", draft); err != nil {
			return nil, err
		}
		capped, err := runtime.indexCapped(ctx, draft)
		if err != nil {
			return nil, err
		}
		draft = capped
	}
	target := SearchTarget{state: &searchTargetState{members: []*searchTargetMember{{runtime: runtime, selected: selected}}, mode: request.SyncMode}}
	defer target.state.closed.Store(true)
	run := func(q query.SearchQuery) ([]string, error) {
		q.SyncMode = request.SyncMode
		// Hits outside candidate are dropped after the search, so it fetches a
		// margin over the slots it fills.
		q.Limit = gatherSearchLimit * 5
		found, _, err := a.searchTarget(ctx, target, q)
		if err != nil {
			return nil, err
		}
		// Equal scores come in no fixed order, so the ID breaks them: a repeated
		// check reads alike.
		slices.SortStableFunc(found.Entries, func(x, y query.SearchEntry) int {
			switch {
			case x.Score > y.Score:
				return -1
			case x.Score < y.Score:
				return 1
			}
			if x.Entry == nil || y.Entry == nil {
				return 0
			}
			return strings.Compare(x.Entry.ID, y.Entry.ID)
		})
		var ids []string
		for _, hit := range found.Entries {
			if hit.Entry != nil && candidate[hit.Entry.ID] && len(ids) < gatherSearchLimit {
				ids = append(ids, hit.Entry.ID)
			}
		}
		return ids, nil
	}
	var reads []model.GroundingRead
	if draft != "" {
		ids, err := run(query.SearchQuery{Phrase: draft})
		if err != nil {
			return nil, fmt.Errorf("sdd: gather: semantic search with the draft's text: %w", err)
		}
		reads = append(reads, model.GroundingRead{Mode: model.GroundingSemantic, IDs: ids})
	}
	if phrase != "" {
		ids, err := run(query.SearchQuery{Phrase: phrase})
		if err != nil {
			return nil, fmt.Errorf("sdd: gather: semantic search: %w", err)
		}
		reads = append(reads, model.GroundingRead{Mode: model.GroundingSemantic, Text: phrase, IDs: ids})
	}
	for _, term := range request.Terms {
		ids, err := run(query.SearchQuery{Terms: []string{regexp.QuoteMeta(term)}, WholeWords: true})
		if err != nil {
			return nil, fmt.Errorf("sdd: gather: keyword search %q: %w", term, err)
		}
		reads = append(reads, model.GroundingRead{Mode: model.GroundingKeyword, Text: term, IDs: ids})
	}
	return reads, nil
}

// The reachability probe gather embeds before any search: its text, and its
// time limit, long enough for a cold model load and short against
// embedding.timeout.
const (
	embedProbeText    = "grounding reachability probe"
	embedProbeTimeout = 30 * time.Second
)

// searchesByMeaning reports whether the project has an embedding model and a
// search index: without them a check runs its keyword searches and topic
// listings alone (20261121-233807-d-tac-4iw).
func (r *ProjectRuntime) searchesByMeaning() bool {
	return r.options.Embedder != nil && r.options.SearchIndex != nil
}

// checkEmbedder fails fast when the configured embedding model does not
// answer within embedProbeTimeout, naming the model and its host.
func checkEmbedder(ctx context.Context, runtime *ProjectRuntime) error {
	probe, cancel := context.WithTimeout(ctx, embedProbeTimeout)
	defer cancel()
	_, err := runtime.options.Embedder.Embed(probe, embed.Request{Purpose: embed.PurposeQuery, Texts: []string{embedProbeText}})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
		return fmt.Errorf("sdd: gather: the embedding model%s did not answer within %s: %w", embedderAt(err), embedProbeTimeout, err)
	}
	return fmt.Errorf("sdd: gather: the embedding model%s does not answer: %w", embedderAt(err), err)
}

// embedderAt names the model and host an embedding error is attributed to,
// with a leading space; empty when it carries none.
func embedderAt(err error) string {
	attributed, ok := errors.AsType[*llm.Error](err)
	if !ok || attributed.Identity.Model == "" {
		return ""
	}
	at := " " + attributed.Identity.Provider + "/" + attributed.Identity.Model
	if attributed.Identity.Host != "" {
		at += " at " + attributed.Identity.Host
	}
	return at
}

// WarmEmbedder embeds gather's reachability probe once, so a local model is
// loaded before the first capture reads; nothing without an embedding model.
func (a *Application) WarmEmbedder(ctx context.Context, identity RequestIdentity, project ProjectID) error {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return err
	}
	if runtime.options.Embedder == nil {
		return nil
	}
	_, err = runtime.options.Embedder.Embed(ctx, embed.Request{Purpose: embed.PurposeQuery, Texts: []string{embedProbeText}})
	if err != nil {
		return fmt.Errorf("sdd: warming the embedding model%s: %w", embedderAt(err), err)
	}
	return nil
}

func uniqueIDs(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

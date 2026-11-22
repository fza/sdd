package application

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
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
	ledger, thresholds, err := runtime.judgeCalibration(ctx, judge)
	if err != nil {
		return GatherResult{}, err
	}
	split := model.SplitByLedger(candidates, request.Subject, ledger, judge, thresholds)
	if len(split.NotRelevant) > 0 {
		setAside = append(setAside, model.GatherSetAside{Reason: model.SetAsideJudged, IDs: split.NotRelevant})
	}
	labels := ledger.Labels(judge)
	var held map[string]uint8
	var floor *model.FarFloor
	below := map[string]bool{}
	subject := ""
	if strings.TrimSpace(request.Draft) != "" {
		subject = model.RelevanceText(request.Kind, request.Topics, "", request.Draft)
		key := model.RelevanceSubjectHash(subject)
		held, _ = request.reading.Scores(judge, key)
		// A floor that went off sets nothing aside, verdicts held from before included.
		if prefilter.Scorer != nil && !model.JudgeFloorHealth(labels).Off {
			below = model.StandingBelowFloor(graph, request.References, split.Undecided, request.reading.BelowFloor(judge, key))
			listed := slices.Concat(request.Answered, request.References, opened, []string{request.Subject})
			for _, read := range reads {
				listed = append(listed, read.IDs...)
			}
			floor = model.NewFarFloor(graph, live, model.FarDraft{Subject: key, Topics: request.Topics, References: request.References}, listed)
		}
	}

	pass := prefilterPass{status: model.PrefilterOff, note: "ids only: no search, listing or judge call", scores: held}
	if !openOnly {
		budget := judgeBudget{limit: policy.JudgeTimeout, target: model.JudgeTarget(len(split.Undecided), policy.JudgeMinimum), wait: request.Wait, unattended: request.unattended}
		endCheck := a.liveChecks.begin()
		pass, err = runPrefilter(ctx, prefilter, policy, budget, subject, graph, split.Undecided, held, floor)
		endCheck()
		if err != nil {
			return GatherResult{}, err
		}
	}
	var shortfall *model.JudgeShortfall
	if pass.judged {
		state, err := runtime.judgeState(ctx, ledger, judge, pass.broken)
		if err != nil {
			return GatherResult{}, err
		}
		limit := policy.JudgeTimeout
		if limit <= 0 || request.Wait {
			limit = pass.waited
		}
		if short, ok := model.NewJudgeShortfall(pass.scored, len(split.Undecided), policy.JudgeMinimum, limit, state); ok {
			short.Unattended = request.unattended
			shortfall = &short
			slogutils.FromContext(ctx).Warn("gather: the ranking judge scored fewer candidates than the minimum",
				"judge", judge, "judged", short.Judged, "listed", short.Listed, "minimum", short.Minimum, "cause", short.Cause, "waited", short.Waited, "unattended", short.Unattended)
		}
	}
	for _, id := range pass.below {
		below[id] = true
	}
	seed := model.NewCorpusMark(liveIDs).Hash
	if request.since != nil {
		seed = request.since.Hash
	}
	marked := map[string]string{}
	for _, candidate := range split.Relevant {
		marked[candidate.Entry.ID] = ""
	}
	lines := model.PlaceLines(model.LineInput{
		Candidates: slices.Concat(split.Relevant, split.Undecided),
		Scores:     pass.scores, BelowFloor: below, Thresholds: thresholds,
		Widened:    model.JudgeRankingHealth(labels).Weak(),
		SelfJudged: !openOnly && subject != "" && (pass.status == model.PrefilterOff || pass.status == model.PrefilterFailed || shortfall != nil),
		Hops:       model.RefHops(graph, request.References), Marked: marked, Seed: seed,
	})
	setAside = append(setAside, lines.SetAside...)
	floored := 0
	for _, group := range lines.SetAside {
		if group.Reason == model.SetAsideFloor {
			floored = len(group.IDs)
		}
	}
	if pass.floored || floored > 0 {
		clause := fmt.Sprintf("the far floor set aside %d", floored)
		if pass.note != "" {
			clause = "; " + clause
		}
		pass.note += clause
	}
	served := &query.GatherResult{
		Graph: graph, Reads: reads, Corpus: model.NewCorpusMark(liveIDs), Prefilter: pass.status, PrefilterNote: pass.note, Shortfall: shortfall,
		Found: len(candidates), Judged: lines.Judged + len(split.NotRelevant), Lines: lines, SetAside: setAside,
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
	var rendered bytes.Buffer
	err = presenters.RenderGather(&rendered, served)
	if err != nil {
		return GatherResult{}, err
	}
	result.Text = strings.TrimRight(rendered.String(), "\n")
	result.payload = model.ReadPayload{Reads: reads, Full: slices.Clone(opened), Shown: slices.Clone(result.SummaryIDs), Marked: lines.MarkedIDs(), Corpus: served.Corpus, Shortfall: shortfall}
	if len(pass.fresh) > 0 || len(lines.Audit) > 0 {
		result.payload.Prefilter = &model.PrefilterPayload{Judge: judge, Forward: prefilter.Scorer != nil && prefilter.Scorer.Direction() == relevance.Forward, Subject: model.RelevanceSubjectHash(subject), Scores: pass.fresh, Below: pass.below, Audit: lines.Audit}
	}
	return result, nil
}

// gatherSearches runs the semantic search with the draft's own text, the
// agent's semantic search and its keyword searches. Each search lists up to
// gatherSearchLimit hits among candidate, the live entries that can become
// candidates, so closed entries and records of who takes part never take a
// slot.
func (a *Application) gatherSearches(ctx context.Context, runtime *ProjectRuntime, selected *readSnapshotSelection, request GatherRequest, candidate map[string]bool) ([]model.GroundingRead, error) {
	draft := strings.TrimSpace(request.Draft)
	if draft != "" || request.Query != "" {
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
	if request.Query != "" {
		ids, err := run(query.SearchQuery{Phrase: request.Query})
		if err != nil {
			return nil, fmt.Errorf("sdd: gather: semantic search: %w", err)
		}
		reads = append(reads, model.GroundingRead{Mode: model.GroundingSemantic, Text: request.Query, IDs: ids})
	}
	for _, term := range request.Terms {
		ids, err := run(query.SearchQuery{Terms: []string{regexp.QuoteMeta(term)}})
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

// checkEmbedder fails fast when no embedding model is configured or the
// configured one does not answer within embedProbeTimeout, naming the model
// and its host.
func checkEmbedder(ctx context.Context, runtime *ProjectRuntime) error {
	if runtime.options.Embedder == nil || runtime.options.SearchIndex == nil {
		return &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: gather needs an embedding model for its semantic search: configure embedding.provider"}
	}
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

package application

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// GroundingPolicy is a project's grounding configuration. A zero limit takes
// its default.
type GroundingPolicy struct {
	Enforce       bool
	DepthDown     int
	DepthUp       int
	BriefAllBelow int
	PrefilterFrom int
	// Prefilter switches the prefilter on where the runtime has one.
	Prefilter bool
	// AutoCalibrate lets a server score the ranking judge's unscored
	// labelled pairs in the background (Application.AutoCalibrate).
	AutoCalibrate bool
	// RecheckResponsible names the participants catch-up shows the recheck
	// list to; empty shows it to everyone.
	RecheckResponsible []string
}

func (p GroundingPolicy) resolved() GroundingPolicy {
	defaults := model.VerifyConfig{}.GroundingPolicy()
	for _, field := range []struct {
		value    *int
		fallback int
	}{{&p.DepthDown, defaults.DepthDown}, {&p.DepthUp, defaults.DepthUp}, {&p.BriefAllBelow, defaults.BriefAllBelow}, {&p.PrefilterFrom, defaults.PrefilterFrom}} {
		if *field.value == 0 {
			*field.value = field.fallback
		}
	}
	return p
}

// gatherSearchLimit is how many hits one grounding search lists. Listings are
// never bounded: a topic's live population is the read.
const gatherSearchLimit = 20

// GatherRequest asks for a grounding serve. Draft, Kind and Topics describe
// the draft; IDs opens entries in full; Query and Terms replace the query
// writer with explicit searches, a path only an uncredited read takes.
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
}

// GatherResult is the rendered serve and the entries it served.
type GatherResult struct {
	Project    ProjectRef
	Text       string
	FullIDs    []string
	SummaryIDs []string
	payload    model.ReadPayload
}

// Gather serves the reading of a draft: below the configured size every live
// brief and no search, above it the semantic search, the keyword searches
// and the live topic listings, each run once on one snapshot. It credits
// nothing; a capture is credited through WorkflowSession.Gather.
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
	case len(live) < policy.BriefAllBelow:
		reads = append(reads, model.GroundingRead{Mode: model.GroundingBriefs, IDs: liveIDs})
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
		for _, label := range request.Topics {
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
		return GatherResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf(
			"sdd: gather needs the draft: the graph holds %d live entries, at or above verify.brief_all_below (%d), so the reading searches from the draft's text", len(live), policy.BriefAllBelow)}
	}
	landed := request.since.Since(liveIDs)
	if len(landed) > 0 {
		reads = append(reads, model.GroundingRead{Mode: model.GroundingNew, IDs: landed})
	}
	if len(opened) > 0 {
		reads = append(reads, model.GroundingRead{Mode: model.GroundingOpened, IDs: opened})
	}

	candidates, setAside := model.GatherCandidates(graph, live, reads, opened, request.Answered)
	candidates = slices.DeleteFunc(candidates, func(c model.GatherCandidate) bool { return c.Entry.ID == request.Subject })
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

	pass, err := runPrefilter(ctx, prefilter, policy, subject, graph, split.Undecided, held, floor)
	if err != nil {
		return GatherResult{}, err
	}
	for _, id := range pass.below {
		below[id] = true
	}
	placed := model.PlacePrefiltered(split.Undecided, pass.scores, below, thresholds, model.JudgeRankingHealth(labels).Weak())
	if len(placed.Listed)+len(placed.Counted) > 0 {
		pass.note += fmt.Sprintf("; below its first recall stage, so the top %d scored stay to judge, %d more go by title and %d are counted", len(placed.ToJudge)-unscored(placed.ToJudge), len(placed.Listed), len(placed.Counted))
	}
	seed := model.NewCorpusMark(liveIDs).Hash
	if request.since != nil {
		seed = request.since.Hash
	}
	audit, kept := model.AuditSample(slices.Concat(placed.SetAside, placed.BelowFloor), seed)
	var filtered, floored []string
	for _, id := range kept {
		if below[id] {
			floored = append(floored, id)
		} else {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) > 0 {
		setAside = append(setAside, model.GatherSetAside{Reason: model.SetAsideFiltered, IDs: filtered})
	}
	if len(floored) > 0 {
		setAside = append(setAside, model.GatherSetAside{Reason: model.SetAsideFloor, IDs: floored})
	}
	if pass.floored || len(floored) > 0 {
		clause := fmt.Sprintf("the far floor set aside %d", len(floored))
		if pass.note != "" {
			clause = "; " + clause
		}
		pass.note += clause
	}
	var samples []model.GatherCandidate
	for _, candidate := range split.Undecided {
		if slices.Contains(audit, candidate.Entry.ID) {
			samples = append(samples, candidate)
		}
	}
	candidates = model.InsertBlind(placed.ToJudge, samples, seed)
	served := &query.GatherResult{
		Graph: graph, Reads: reads, Corpus: model.NewCorpusMark(liveIDs), Prefilter: pass.status, PrefilterNote: pass.note,
		ToJudge: candidates, Listed: placed.Listed, Counted: len(placed.Counted), SetAside: setAside,
	}
	if request.Held != nil {
		served.Held, served.Served = request.Held, request.Served
	}
	full := slices.Clone(opened)
	for _, candidate := range append(split.Relevant, placed.Relevant...) {
		full = append(full, candidate.Entry.ID)
	}
	if len(full) > query.GatherFullCap {
		for _, id := range full[query.GatherFullCap:] {
			served.Overflow = append(served.Overflow, graph.ByID[id])
		}
		full = full[:query.GatherFullCap]
	}
	if len(full) > 0 {
		shown, err := snapshot.finder.Show(query.ShowQuery{IDs: full})
		if err != nil {
			return GatherResult{}, err
		}
		served.Full = shown
	}
	result := GatherResult{Project: runtime.options.Project, FullIDs: slices.Clone(full)}
	for _, candidate := range candidates {
		result.SummaryIDs = append(result.SummaryIDs, candidate.Entry.ID)
	}
	for _, e := range served.Overflow {
		result.SummaryIDs = append(result.SummaryIDs, e.ID)
	}
	var rendered bytes.Buffer
	err = presenters.RenderGather(&rendered, served)
	if err != nil {
		return GatherResult{}, err
	}
	result.Text = strings.TrimRight(rendered.String(), "\n")
	result.payload = model.ReadPayload{Reads: reads, Full: slices.Clone(full), Shown: slices.Clone(result.SummaryIDs), Corpus: served.Corpus}
	if len(pass.fresh) > 0 || len(audit) > 0 {
		result.payload.Prefilter = &model.PrefilterPayload{Judge: judge, Forward: prefilter.Scorer != nil && prefilter.Scorer.Direction() == relevance.Forward, Subject: model.RelevanceSubjectHash(subject), Scores: pass.fresh, Below: pass.below, Audit: audit}
	}
	return result, nil
}

// gatherSearches runs the semantic search and the keyword searches. The
// embedder is checked before the query writer spends any LLM tokens. Each
// search lists up to gatherSearchLimit hits among candidate, the live entries
// that can become candidates, so closed entries and records of who takes part
// never take a slot.
func (a *Application) gatherSearches(ctx context.Context, runtime *ProjectRuntime, selected *readSnapshotSelection, request GatherRequest, candidate map[string]bool) ([]model.GroundingRead, error) {
	explicit := request.Query != "" || len(request.Terms) > 0
	if !explicit || request.Query != "" {
		if err := checkEmbedder(ctx, runtime); err != nil {
			return nil, err
		}
	}
	queries := llmops.GatherQueries{Query: request.Query, Terms: request.Terms}
	if !explicit {
		if err := secrets.CheckText("the draft", request.Draft); err != nil {
			return nil, err
		}
		written, err := llmops.GatherQuery(ctx, runtime.options.LLM, llmops.GatherQueryDraft{Kind: request.Kind, Topics: request.Topics, Text: request.Draft})
		if err != nil {
			return nil, fmt.Errorf("sdd: gather: %w", err)
		}
		queries = written
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
		slices.SortStableFunc(found.Entries, func(x, y query.SearchEntry) int {
			switch {
			case x.Score > y.Score:
				return -1
			case x.Score < y.Score:
				return 1
			}
			return 0
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
	if queries.Query != "" {
		ids, err := run(query.SearchQuery{Phrase: queries.Query})
		if err != nil {
			return nil, fmt.Errorf("sdd: gather: semantic search: %w", err)
		}
		reads = append(reads, model.GroundingRead{Mode: model.GroundingSemantic, Text: queries.Query, IDs: ids})
	}
	for _, term := range queries.Terms {
		ids, err := run(query.SearchQuery{Terms: []string{regexp.QuoteMeta(term)}})
		if err != nil {
			return nil, fmt.Errorf("sdd: gather: keyword search %q: %w", term, err)
		}
		reads = append(reads, model.GroundingRead{Mode: model.GroundingKeyword, Text: term, IDs: ids})
	}
	return reads, nil
}

// checkEmbedder fails fast when no embedding model is configured or the
// configured one does not answer.
func checkEmbedder(ctx context.Context, runtime *ProjectRuntime) error {
	if runtime.options.Embedder == nil || runtime.options.SearchIndex == nil {
		return &ApplicationError{Code: ErrorInvalidArgument, Message: "sdd: gather needs an embedding model for its semantic search: configure embedding.provider"}
	}
	if _, err := runtime.options.Embedder.Embed(ctx, embed.Request{Purpose: embed.PurposeQuery, Texts: []string{"grounding reachability probe"}}); err != nil {
		return fmt.Errorf("sdd: gather: the embedding model does not answer: %w", err)
	}
	return nil
}

func unscored(candidates []model.GatherCandidate) int {
	n := 0
	for _, candidate := range candidates {
		if candidate.Score == nil {
			n++
		}
	}
	return n
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

package application

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// deepTargets are the entries a capture's draft declares: every ref,
// supersede, close and approve target.
func deepTargets(store *engine.Store) []string {
	var targets []string
	for _, ref := range workflowStoreDocuments(store, "refs") {
		if id, _ := ref["id"].(string); id != "" {
			targets = append(targets, id)
		}
	}
	return slices.Concat(targets, workflowStoreStrings(store, "supersedes"), workflowStoreStrings(store, "closes"), workflowStoreStrings(store, "approves"))
}

// rankingPlan is the deep reading placed with the scores it holds: the
// ranking judge of the session's host and the entries whose rank needs a
// score from it.
type rankingPlan struct {
	runtime   *ProjectRuntime
	graph     *model.Graph
	judge     Prefilter
	subject   string
	draft     model.VerifierDraft
	input     model.VerifierReading
	placement model.VerifierPlacement
	unscored  []string
	// recorded are the entries whose score only a background pass's record
	// holds (rankedScores), not yet the reading.
	recorded []string
	// mark is the final draft the searches ran against; nil without them.
	mark *model.DraftMark
}

// planRanking places an instance's reading by the judgements the capture
// keeps and the scores its ranking judge gave the draft, under the judge's
// calibration; a weak ranking health widens the reading's tiers. With
// search, the capture's searches run again against the final draft, their
// merged order places the candidates, and the kept low judgements due a
// second look are left to score (20261121-233807-d-tac-4iw).
func (w *WorkflowSession) planRanking(instance string, graph *model.Graph, store *engine.Store, credited *model.Reading, search bool) (rankingPlan, error) {
	runtime, err := w.targetRuntime(w.instanceProject(instance), AccessRead)
	if err != nil {
		return rankingPlan{}, err
	}
	client, err := w.attachedClient(w.ctx)
	if err != nil {
		return rankingPlan{}, err
	}
	plan := rankingPlan{runtime: runtime, graph: graph, judge: runtime.rankingJudge(client), draft: model.VerifierDraft{Targets: deepTargets(store)}}
	ledger, thresholds, err := runtime.judgeCalibration(w.ctx, plan.judge.Judge)
	if err != nil {
		return rankingPlan{}, err
	}
	draft := w.draftFromStore(store)
	plan.subject = model.RelevanceText(draft.Kind, draft.Topics, "", draft.Body)
	subject := model.RelevanceSubjectHash(plan.subject)
	reading := captureReadingOf(store, credited)
	plan.input = model.VerifierReading{Listed: reading.listed, Full: reading.full, Verdicts: storeVerdictIDs(store), Scores: maps.Clone(credited.Kept(plan.judge.Judge)), Thresholds: thresholds}
	if plan.input.Scores == nil {
		plan.input.Scores = map[string]uint8{}
	}
	if search {
		err := w.searchAgain(&plan, runtime, store, draft, credited)
		if err != nil {
			return rankingPlan{}, err
		}
	}
	if plan.judge.Scorer != nil {
		ranked, err := w.rankedScores(instance, plan.judge.Judge, subject)
		if err != nil {
			return rankingPlan{}, err
		}
		for id, score := range ranked {
			if _, ok := plan.input.Scores[id]; !ok {
				plan.input.Scores[id] = score
				plan.recorded = append(plan.recorded, id)
			}
		}
	}
	if plan.judge.Judge != "" {
		plan.input.Widened = model.JudgeRankingHealth(ledger.Labels(plan.judge.Judge)).Weak()
	}
	plan.input.Seed = model.NewCorpusMark(reading.listed).Hash
	if credited != nil && credited.Corpus != nil {
		plan.input.Seed = credited.Corpus.Hash
	}
	plan.placement = model.PlaceVerifierReading(graph, plan.draft, plan.input)
	plan.unscored = model.VerifierUnscored(plan.placement, plan.input)
	return plan, nil
}

// searchAgain runs the capture's searches against the final draft for the
// deep verifier's reading: the search by meaning with its text, each search
// phrase and keyword term the capture's checks passed, and the listing of
// each of its topics. It orders the candidates no search found by their
// similarity to the draft and takes from the plan's scores the kept low
// judgements due a second look (model.SecondLook).
func (w *WorkflowSession) searchAgain(plan *rankingPlan, runtime *ProjectRuntime, store *engine.Store, draft EntryDraft, credited *model.Reading) (err error) {
	if strings.TrimSpace(draft.Body) == "" {
		return nil
	}
	target, _ := w.effectiveTarget(store)
	selected, err := acquireSnapshotForSearch(w.ctx, runtime, target.Branch, "")
	if err != nil {
		return err
	}
	defer selected.releaseInto(&err)
	live := model.GroundingPopulation(plan.graph)
	candidate := make(map[string]bool, len(live))
	for _, e := range live {
		candidate[e.ID] = true
	}
	searches := []GatherRequest{{SyncMode: SearchSyncLocal, Draft: draft.Body}}
	var terms []string
	for _, read := range credited.Reads {
		switch {
		case read.Mode == model.GroundingSemantic && read.Text != "" && !slices.ContainsFunc(searches, func(r GatherRequest) bool { return r.Query == read.Text }):
			searches = append(searches, GatherRequest{SyncMode: SearchSyncLocal, Query: read.Text})
		case read.Mode == model.GroundingKeyword && !slices.Contains(terms, read.Text):
			terms = append(terms, read.Text)
		}
	}
	searches[0].Terms = terms
	var reads []model.GroundingRead
	for _, search := range searches {
		found, err := w.app.gatherSearches(w.ctx, runtime, selected, search, candidate)
		if err != nil {
			return fmt.Errorf("the verifier's reading: %w", err)
		}
		reads = append(reads, found...)
	}
	for _, label := range draft.Topics {
		path, err := model.ParseTopicPath(label)
		if err != nil {
			continue
		}
		var ids []string
		for _, e := range model.TopicListing(plan.graph, live, path) {
			ids = append(ids, e.ID)
		}
		reads = append(reads, model.GroundingRead{Mode: model.GroundingListing, Text: path.String(), IDs: ids})
	}
	plan.input.Reads = reads
	candidates := model.VerifierCandidates(plan.graph, plan.draft, plan.input)
	plan.input.Similarity, err = plan.runtime.draftSimilarity(w.ctx, plan.subject, model.Unsearched(candidates, reads))
	if err != nil {
		return fmt.Errorf("the verifier's reading: similarity to the draft: %w", err)
	}
	candidates = model.VerifierCandidates(plan.graph, plan.draft, plan.input)

	plan.mark = &model.DraftMark{Topics: slices.Clone(draft.Topics), References: captureAnswered(store)}
	if len(reads) > 0 && reads[0].Mode == model.GroundingSemantic && reads[0].Text == "" {
		plan.mark.Nearest = slices.Clone(reads[0].IDs[:min(model.SecondLookTop, len(reads[0].IDs))])
	}
	judged := credited.JudgedDraft()
	moved := model.DraftMoved(judged, plan.mark.Nearest)
	plan.mark.Judged = judged == nil || moved
	order := make([]string, len(candidates))
	for i, c := range candidates {
		order[i] = c.Entry.ID
	}
	due := model.SecondLook{
		Graph: plan.graph, Live: live, Kept: credited.Stale(plan.judge.Judge, model.RelevanceSubjectHash(plan.subject)), Thresholds: plan.input.Thresholds, Order: order,
		Previous: credited.LastDraft(), Current: *plan.mark, Moved: moved,
	}.Due()
	for _, id := range due {
		delete(plan.input.Scores, id)
	}
	return nil
}

func (w *WorkflowSession) rankingKey(instance string, plan rankingPlan) rankingKey {
	return rankingKey{session: w.session.ID, instance: instance, judge: plan.judge.Judge, subject: model.RelevanceSubjectHash(plan.subject)}
}

// startRanking has the plan's judge score its unscored entries in the
// background, bounded by the ranking timeout.
func (w *WorkflowSession) startRanking(instance string, plan rankingPlan) error {
	if err := secrets.CheckText("the draft", plan.subject); err != nil {
		return err
	}
	candidates := make([]relevance.Candidate, 0, len(plan.unscored))
	for _, id := range plan.unscored {
		e := plan.graph.ByID[id]
		candidates = append(candidates, relevance.Candidate{ID: id, Text: model.EntryRelevanceText(plan.graph, e), Line: model.EntryRankingLine(e)})
	}
	run := plan.runtime.rankings.begin(w.rankingKey(instance, plan), plan.judge, plan.subject, candidates, plan.runtime.rankingTimeout())
	if run == nil {
		return nil
	}
	// The pass outlives the request that starts it, which returns first.
	ctx, release := w.app.background.detached(w.ctx)
	identity, session := w.identity, w.ID()
	payload := model.PrefilterPayload{Judge: plan.judge.Judge, Forward: plan.judge.Scorer.Direction() == relevance.Forward, Subject: model.RelevanceSubjectHash(plan.subject)}
	started := w.app.background.start(func() {
		defer release()
		payload.Scores = run.score(ctx).fresh
		// What a pass cut short by Shutdown scored is kept too.
		err := w.app.keepRanking(context.WithoutCancel(ctx), identity, session, instance, payload)
		if err != nil {
			slogutils.FromContext(ctx).Error("keeping the scores of a background ranking pass failed; a later reading scores them again", "session", session, "instance", instance, "error", err)
		}
	})
	if !started {
		release()
		run.abandon()
	}
	return nil
}

// keepRanking keeps what a background pass scored for instance in the
// ranking store (RankingStore), so a later reading and a server started anew
// find it (rankedScores), unless it scored nothing or the session ended.
func (a *Application) keepRanking(ctx context.Context, identity RequestIdentity, session SessionID, instance string, scored model.PrefilterPayload) error {
	if len(scored.Scores) == 0 {
		return nil
	}
	_, _, stored, err := a.resolveSession(ctx, identity, session)
	if err != nil {
		return err
	}
	if stored.Metadata.Ended != nil {
		return nil
	}
	record, err := json.Marshal(scored)
	if err != nil {
		return err
	}
	return a.rankings.AppendRanking(ctx, SessionRef{Subject: stored.Metadata.Subject, Session: session}, instance, record)
}

// workflowRankingCode marks a ranking record a session's log may hold. No
// call writes one; rankedScores reads those a log holds.
const workflowRankingCode = "workflow_ranking_scores"

// loggedRanking is the payload of a workflowRankingCode event.
type loggedRanking struct {
	Instance  string                 `json:"instance"`
	Prefilter model.PrefilterPayload `json:"prefilter"`
}

// rankedScores are the scores the background ranking passes of instance
// recorded for judge against subject, the highest per entry.
func (w *WorkflowSession) rankedScores(instance, judge, subject string) (map[string]uint8, error) {
	_, _, stored, err := w.app.resolveSession(w.ctx, w.identity, w.ID())
	if err != nil {
		return nil, err
	}
	var recorded []model.PrefilterPayload
	for _, event := range stored.Events {
		if event.Code != workflowRankingCode {
			continue
		}
		if !SupportedSessionCodecVersion(event.CodecVersion) {
			return nil, &ApplicationError{Code: ErrorMigrationRequired, Message: "unsupported ranking record codec", Version: event.CodecVersion}
		}
		var logged loggedRanking
		err := json.Unmarshal(event.Payload, &logged)
		if err != nil {
			return nil, fmt.Errorf("decoding a ranking record: %w", err)
		}
		if logged.Instance == instance {
			recorded = append(recorded, logged.Prefilter)
		}
	}
	kept, err := w.app.rankings.Rankings(w.ctx, SessionRef{Subject: stored.Metadata.Subject, Session: w.ID()}, instance)
	if err != nil {
		return nil, fmt.Errorf("reading the ranking records of %s: %w", instance, err)
	}
	for _, record := range kept {
		var scored model.PrefilterPayload
		err := json.Unmarshal(record, &scored)
		if err != nil {
			return nil, fmt.Errorf("decoding a ranking record: %w", err)
		}
		recorded = append(recorded, scored)
	}
	scores := map[string]uint8{}
	for _, scored := range recorded {
		if model.CanonicalJudge(scored.Judge) != model.CanonicalJudge(judge) || scored.Subject != subject {
			continue
		}
		for id, score := range scored.Scores {
			if held, ok := scores[id]; !ok || score > held {
				scores[id] = score
			}
		}
	}
	return scores, nil
}

// rankInBackground starts ranking a capture's reading once its draft and
// listed set are known, at a gather after the draft or as assemble passes,
// so the scores are ready when the deep verifier's reading is built; only
// for a draft the deep verifier checks.
func (w *WorkflowSession) rankInBackground(instance string) error {
	inst, ok := w.session.Instance(instance)
	if !ok {
		return nil
	}
	policy, _, err := w.deepPolicy(inst.ID, inst.Store)
	if err != nil || policy.Mode != model.VerifierDeep {
		return err
	}
	if body, _ := workflowStoreString(inst.Store, "body"); body == "" {
		return nil
	}
	graph, err := w.graphs.currentForInstance(inst.ID, inst.Store)
	if err != nil {
		return err
	}
	plan, err := w.planRanking(inst.ID, graph, inst.Store, inst.Reading(), false)
	if err != nil || plan.judge.Scorer == nil || len(plan.unscored) == 0 {
		return err
	}
	return w.startRanking(inst.ID, plan)
}

// verifierPlacement is the deep verifier's placement of the reading
// (model.PlaceVerifierReading), over the capture's searches run again
// against the final draft. The ranking judge scores what the reading holds
// no judgement for, or a low one due a second look, where the rank decides,
// the pass begun at gather collected, each fresh score and the final draft
// recorded in the reading, which publish writes to the ledger. What the
// judge left unscored by the time limit keeps its kept judgement, or its
// place in the merged order.
func (w *WorkflowSession) verifierPlacement(ctx *engine.Context) (verifierPlaced, error) {
	plan, err := w.planRanking(ctx.Instance, ctx.Graph, ctx.Store, ctx.Reading, true)
	if err != nil {
		return verifierPlaced{}, err
	}
	kept := maps.Clone(ctx.Reading.Kept(plan.judge.Judge))
	logger := slogutils.FromContext(w.ctx)
	if plan.judge.Scorer != nil {
		fresh := map[string]uint8{}
		for _, id := range plan.recorded {
			fresh[id] = plan.input.Scores[id]
		}
		if len(plan.unscored) > 0 {
			if err := w.startRanking(ctx.Instance, plan); err != nil {
				return verifierPlaced{}, err
			}
			scored := plan.runtime.rankings.collect(w.ctx, w.rankingKey(ctx.Instance, plan))
			if scored.unanswered > 0 || scored.oversize > 0 || scored.late {
				logger.Warn("the ranking judge left entries of the verifier's reading unscored; they rank by similarity to the draft", "judge", plan.judge.Judge, "unanswered", scored.unanswered, "oversize", scored.oversize, "past the time limit", scored.late, "failures", scored.failures)
			}
			for id, score := range scored.fresh {
				if _, held := plan.input.Scores[id]; !held {
					fresh[id] = score
				}
			}
		}
		grounding := model.ReadPayload{Draft: plan.mark}
		if len(fresh) > 0 {
			grounding.Prefilter = &model.PrefilterPayload{Judge: plan.judge.Judge, Forward: plan.judge.Scorer.Direction() == relevance.Forward, Subject: model.RelevanceSubjectHash(plan.subject), Scores: fresh}
		}
		if !grounding.Empty() {
			if err := w.session.Record(engine.Read{Tool: "verify", Instance: ctx.Instance, Grounding: grounding}); err != nil {
				return verifierPlaced{}, err
			}
		}
		maps.Copy(plan.input.Scores, fresh)
	}
	for id, score := range kept {
		if _, scored := plan.input.Scores[id]; !scored {
			plan.input.Scores[id] = score
		}
	}
	plan.placement = model.PlaceVerifierReading(plan.graph, plan.draft, plan.input)
	return verifierPlaced{placement: plan.placement, input: plan.input, judge: plan.judge.Judge}, nil
}

// verifierPlaced is the deep verifier's placed reading, the input it was
// placed from and the judge that ranked it.
type verifierPlaced struct {
	placement model.VerifierPlacement
	input     model.VerifierReading
	judge     string
}

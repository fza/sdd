package application

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

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
	runtime  *ProjectRuntime
	graph    *model.Graph
	judge    Prefilter
	subject  string
	draft    model.VerifierDraft
	input    model.VerifierReading
	slots    []model.VerifierSlot
	unscored []string
	// recorded are the entries whose score only a background pass's record
	// holds (rankedScores), not yet the reading.
	recorded []string
}

// planRanking places an instance's reading by the scores its ranking judge
// gave the draft, under the judge's calibration; a weak ranking health
// widens the reading's tiers.
func (w *WorkflowSession) planRanking(instance string, graph *model.Graph, store *engine.Store, credited *model.Reading) (rankingPlan, error) {
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
	held, _ := credited.Scores(plan.judge.Judge, subject)
	reading := captureReadingOf(store, credited)
	plan.input = model.VerifierReading{Listed: reading.listed, Full: reading.full, Verdicts: storeVerdictIDs(store), Scores: maps.Clone(held), Thresholds: thresholds}
	if plan.input.Scores == nil {
		plan.input.Scores = map[string]uint8{}
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
	plan.slots = model.PlaceVerifierReading(graph, plan.draft, plan.input)
	plan.unscored = model.VerifierUnscored(plan.slots, plan.input)
	return plan, nil
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
	plan, err := w.planRanking(inst.ID, graph, inst.Store, inst.Reading())
	if err != nil || plan.judge.Scorer == nil || len(plan.unscored) == 0 {
		return err
	}
	return w.startRanking(inst.ID, plan)
}

// verifierPlacement is the deep verifier's placement of the reading
// (model.PlaceVerifierReading). The ranking judge scores what the reading
// holds no score for where the rank decides, the pass begun at gather
// collected, each fresh score recorded in the reading, which publish writes
// to the ledger. What the judge left unscored by the time limit ranks by
// similarity to the draft.
func (w *WorkflowSession) verifierPlacement(ctx *engine.Context) (verifierPlaced, error) {
	plan, err := w.planRanking(ctx.Instance, ctx.Graph, ctx.Store, ctx.Reading)
	if err != nil {
		return verifierPlaced{}, err
	}
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
		if len(fresh) > 0 {
			payload := &model.PrefilterPayload{Judge: plan.judge.Judge, Forward: plan.judge.Scorer.Direction() == relevance.Forward, Subject: model.RelevanceSubjectHash(plan.subject), Scores: fresh}
			if err := w.session.Record(engine.Read{Tool: "verify", Instance: ctx.Instance, Grounding: model.ReadPayload{Prefilter: payload}}); err != nil {
				return verifierPlaced{}, err
			}
			maps.Copy(plan.input.Scores, fresh)
			plan.slots = model.PlaceVerifierReading(plan.graph, plan.draft, plan.input)
		}
	}
	if still := model.VerifierUnscored(plan.slots, plan.input); len(still) > 0 {
		similarity, err := plan.runtime.draftSimilarity(w.ctx, plan.subject, still)
		if err != nil {
			logger.Warn("the verifier's reading keeps listed order for the entries no judge scored: similarity to the draft failed", "error", err)
		} else if len(similarity) > 0 {
			plan.input.Similarity = similarity
			plan.slots = model.PlaceVerifierReading(plan.graph, plan.draft, plan.input)
		}
	}
	return verifierPlaced{slots: plan.slots, input: plan.input, judge: plan.judge.Judge}, nil
}

// verifierPlaced is the deep verifier's placed reading, the input it was
// placed from and the judge that ranked it.
type verifierPlaced struct {
	slots []model.VerifierSlot
	input model.VerifierReading
	judge string
}

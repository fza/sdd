package application

import (
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
	held, _ := credited.Scores(plan.judge.Judge, model.RelevanceSubjectHash(plan.subject))
	reading := captureReadingOf(store, credited)
	plan.input = model.VerifierReading{Listed: reading.listed, Full: reading.full, Verdicts: storeVerdictIDs(store), Scores: maps.Clone(held), Thresholds: thresholds}
	if plan.input.Scores == nil {
		plan.input.Scores = map[string]uint8{}
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
	plan.runtime.rankings.start(w.ctx, w.rankingKey(instance, plan), plan.judge, plan.subject, candidates, plan.runtime.rankingTimeout())
	return nil
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
	if len(plan.unscored) > 0 && plan.judge.Scorer != nil {
		if err := w.startRanking(ctx.Instance, plan); err != nil {
			return verifierPlaced{}, err
		}
		scored := plan.runtime.rankings.collect(w.ctx, w.rankingKey(ctx.Instance, plan))
		if scored.unanswered > 0 || scored.oversize > 0 || scored.late {
			logger.Warn("the ranking judge left entries of the verifier's reading unscored; they rank by similarity to the draft", "judge", plan.judge.Judge, "unanswered", scored.unanswered, "oversize", scored.oversize, "past the time limit", scored.late, "failures", scored.failures)
		}
		if len(scored.fresh) > 0 {
			payload := &model.PrefilterPayload{Judge: plan.judge.Judge, Forward: plan.judge.Scorer.Direction() == relevance.Forward, Subject: model.RelevanceSubjectHash(plan.subject), Scores: scored.fresh}
			if err := w.session.Record(engine.Read{Tool: "verify", Instance: ctx.Instance, Grounding: model.ReadPayload{Prefilter: payload}}); err != nil {
				return verifierPlaced{}, err
			}
			maps.Copy(plan.input.Scores, scored.fresh)
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

package application

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// Replay constants: the past entries one background round replays, the
// time one replay's scoring may take, the runes of a replayed text its
// semantic search reads, and the hidden entries one scan after writing
// reads, the closest to the line first.
const (
	replayPerRound = 8
	replayDeadline = 10 * time.Minute
	replayPhrase   = 2000
	scanLines      = 20
)

// replayRound replays past entries as whole checks on judge, the cheap
// ranking judge of a reading without a client, while either hiding rule
// lacks a full proof for it (20261121-210755-d-tac-d5e), and runs the scan
// after writing on the captures whose check it has not read. Before each
// judge call it waits for wait. It reports whether it sent a call.
func (c *autoCalibrator) replayRound(ctx context.Context, runtime *ProjectRuntime, judge Prefilter, wait func(context.Context) error) (sent bool, err error) {
	if runtime.options.Proofs == nil || runtime.options.Embedder == nil || runtime.options.SearchIndex == nil {
		return false, nil
	}
	selected, err := acquireSnapshotForSearch(ctx, runtime, "", "")
	if err != nil {
		return false, err
	}
	defer selected.releaseInto(&err)
	graph := selected.snapshot.graph
	live := model.GroundingPopulation(graph)
	_, records, states, err := runtime.judgeHiding(ctx, judge, len(live))
	if err != nil {
		return false, err
	}
	// The calls below take minutes; the graph read stays in memory.
	if selected.releaseInto(&err); err != nil {
		return false, err
	}
	scanned, err := c.scanTallies(ctx, runtime, graph, judge, records, wait)
	if err != nil || scanned {
		return scanned, err
	}
	if model.ProvenStep(states.threshold.Proof) == model.StepFull && model.ProvenStep(states.floor.Proof) == model.StepFull {
		return false, nil
	}
	done := map[string]bool{}
	for _, check := range model.ValidReplays(records.Replays, judge.Judge, judge.Revision, len(live)) {
		done[check.Subject] = true
	}
	for id := range c.skipped {
		done[id] = true
	}
	for _, e := range model.PickReplays(graph, live, done, replayPerRound) {
		if err := wait(ctx); err != nil {
			return sent, err
		}
		check, ok, err := c.app.replay(ctx, runtime, graph, judge, e, len(live), states.thresholds, wait)
		if err != nil {
			return sent, err
		}
		sent = true
		if !ok {
			c.skipped[e.ID] = true
			continue
		}
		if err := runtime.appendProofs(context.WithoutCancel(ctx), model.ProofRecords{Replays: []model.ReplayCheck{check}}); err != nil {
			return sent, err
		}
	}
	return sent, nil
}

// replay plays e as the draft of a whole check against the entries with
// earlier IDs: its text without the sentences that name its references
// searched semantically and its topics listed, the candidates scored by
// judge with the far entries in every call, and a planted miss handed to
// the scan after writing. ok is false where a call went unanswered; the
// replay then stores nothing and the entry stays to replay.
func (a *Application) replay(ctx context.Context, runtime *ProjectRuntime, graph *model.Graph, judge Prefilter, e *model.Entry, graphSize int, thresholds model.RelevanceThresholds, wait func(context.Context) error) (check model.ReplayCheck, ok bool, err error) {
	log := slogutils.FromContext(ctx)
	text := model.ReplayText(graph, e)
	earlier := model.ReplayPopulation(graph, e)
	allowed := make(map[string]bool, len(earlier))
	for _, other := range earlier {
		allowed[other.ID] = true
	}
	found, err := a.replaySearch(ctx, runtime, text)
	if err != nil {
		return model.ReplayCheck{}, false, fmt.Errorf("replaying %s: semantic search: %w", e.ID, err)
	}
	var semantic []string
	for _, hit := range found.Entries {
		if hit.Entry != nil && allowed[hit.Entry.ID] {
			semantic = append(semantic, hit.Entry.ID)
		}
	}
	reads := []model.GroundingRead{{Mode: model.GroundingSemantic, Text: "replay", IDs: semantic}}
	var topics []string
	for _, topic := range graph.EffectiveTopics(e) {
		topics = append(topics, topic.String())
		var ids []string
		for _, listed := range model.TopicListing(graph, earlier, topic) {
			ids = append(ids, listed.ID)
		}
		reads = append(reads, model.GroundingRead{Mode: model.GroundingListing, Text: topic.String(), IDs: ids})
	}
	candidates, _ := model.GatherCandidates(graph, earlier, reads, nil, nil)
	if len(candidates) > model.ReplayLines {
		candidates = candidates[:model.ReplayLines]
	}
	listed := []string{e.ID}
	pending := make([]relevance.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		listed = append(listed, candidate.Entry.ID)
		pending = append(pending, relevance.Candidate{ID: candidate.Entry.ID, Text: model.EntryRelevanceText(graph, candidate.Entry), Line: model.EntryRankingLine(candidate.Entry)})
	}
	// The replay holds no reference of the draft: they are the answers it checks.
	floor := model.NewFarFloor(graph, earlier, model.FarDraft{Subject: model.RelevanceSubjectHash(text), Topics: topics}, listed)
	scored := scoreCandidates(ctx, judge, text, pending, farCandidates(graph, floor), replayDeadline)
	if scored.unanswered > 0 || scored.oversize > 0 {
		log.Info("replay: the judge left candidates unscored, the entry stays to replay until the next server", "judge", judge.Judge, "entry", e.ID, "unanswered", scored.unanswered, "too long", scored.oversize)
		return model.ReplayCheck{}, false, ctx.Err()
	}
	needed := model.ReplayNeeded(graph, e)
	check = model.ReplayCheck{Subject: e.ID, Judge: judge.Judge, Revision: judge.Revision, At: a.now().UTC(), GraphSize: graphSize}
	var neededListed []string
	for _, candidate := range candidates {
		id := candidate.Entry.ID
		score, judged := scored.fresh[id]
		// A candidate the judge cannot read is never hidden, so it labels nothing.
		if !judged {
			continue
		}
		entry := model.ReplayCandidate{ID: id, Score: score, Needed: slices.Contains(needed, id), Spared: len(candidate.FoundBy) >= 2}
		if mark, marked := scored.marks[id]; marked {
			entry.Far, entry.Lowest, entry.Median = true, model.ScoreByte(mark.line.Lowest), model.ScoreByte(mark.line.Median)
		}
		if entry.Needed {
			neededListed = append(neededListed, id)
		}
		check.Candidates = append(check.Candidates, entry)
	}
	if judge.Scan != nil {
		if err := wait(ctx); err != nil {
			return model.ReplayCheck{}, false, err
		}
		check.Planted = model.ReplayPlant(e.ID, neededListed)
		if check.Planted != "" {
			hidden := plantedScan(graph, check, thresholds)
			flagged, err := judge.Scan.Scan(ctx, model.EntryRelevanceText(graph, e), hidden)
			if err != nil {
				log.Info("replay: the scan after writing failed, the replay plants no miss", "judge", judge.Judge, "entry", e.ID, "err", err)
				check.Planted = ""
			}
			check.Caught = check.Planted != "" && slices.Contains(flagged, check.Planted)
		}
	}
	return check, true, nil
}

// plantedScan is the list a replay's scan reads: the candidates either rule
// hides at its full step that the entry does not reference, closest to the
// line first, with the planted miss among them at a position drawn from
// the subject.
func plantedScan(graph *model.Graph, check model.ReplayCheck, thresholds model.RelevanceThresholds) []relevance.Candidate {
	var hidden []model.ReplayCandidate
	for _, candidate := range check.Candidates {
		below := candidate.Far && !candidate.Spared && candidate.Score <= candidate.Median || thresholds.Valid && model.ScoreValue(candidate.Score) < thresholds.SetAside
		if below && !candidate.Needed {
			hidden = append(hidden, candidate)
		}
	}
	slices.SortStableFunc(hidden, func(x, y model.ReplayCandidate) int { return cmp.Compare(y.Score, x.Score) })
	ids := make([]string, 0, scanLines)
	for _, candidate := range hidden[:min(len(hidden), scanLines-1)] {
		ids = append(ids, candidate.ID)
	}
	sum := sha256.Sum256([]byte(check.Subject + "\nplanted"))
	ids = slices.Insert(ids, int(binary.BigEndian.Uint64(sum[:8])%uint64(len(ids)+1)), check.Planted)
	return scanCandidates(graph, ids)
}

func scanCandidates(graph *model.Graph, ids []string) []relevance.Candidate {
	var out []relevance.Candidate
	for _, id := range ids {
		if e := graph.ByID[id]; e != nil {
			out = append(out, relevance.Candidate{ID: id, Text: model.EntryRelevanceText(graph, e), Line: model.EntryRankingLine(e)})
		}
	}
	return out
}

// scanTallies runs the scan after writing on each capture whose check hid
// entries and that no scan read yet: judge reads the written entry and the
// entries hidden closest to the line, and each one flagged as duplicating
// or contradicting it is a miss of the rule that hid it. A capture the
// graph no longer holds is marked scanned with nothing read. It reports
// whether it sent a call.
func (c *autoCalibrator) scanTallies(ctx context.Context, runtime *ProjectRuntime, graph *model.Graph, judge Prefilter, records model.ProofRecords, wait func(context.Context) error) (bool, error) {
	if judge.Scan == nil {
		return false, nil
	}
	done := map[string]bool{}
	for _, scan := range records.Scans {
		done[scan.Capture] = true
	}
	sent := false
	for _, tally := range records.Tallies {
		if done[tally.Capture] || c.skipped[tally.Capture] {
			continue
		}
		done[tally.Capture] = true
		written := graph.ByID[tally.Capture]
		var ids []string
		rules := map[string]model.HidingRule{}
		for _, rule := range tally.Rules {
			for _, id := range rule.Hidden[:min(len(rule.Hidden), scanLines)] {
				if _, seen := rules[id]; !seen {
					rules[id] = rule.Rule
					ids = append(ids, id)
				}
			}
		}
		out := model.ProofRecords{Scans: []model.Scanned{{Capture: tally.Capture, At: c.app.now().UTC()}}}
		if written != nil && len(ids) > 0 {
			if err := wait(ctx); err != nil {
				return sent, err
			}
			sent = true
			flagged, err := judge.Scan.Scan(ctx, model.EntryRelevanceText(graph, written), scanCandidates(graph, ids))
			if err != nil {
				if ctx.Err() != nil {
					return sent, ctx.Err()
				}
				slogutils.FromContext(ctx).Info("scan after writing failed, the capture stays to scan until the next server", "judge", judge.Judge, "capture", tally.Capture, "err", err)
				c.skipped[tally.Capture] = true
				continue
			}
			for _, id := range flagged {
				out.Misses = append(out.Misses, model.Miss{Capture: tally.Capture, Hidden: id, Rule: rules[id], Detector: model.DetectScan, Judge: tally.Judge, At: c.app.now().UTC()})
			}
		}
		if err := runtime.appendProofs(context.WithoutCancel(ctx), out); err != nil {
			return sent, err
		}
	}
	return sent, nil
}

// replaySearch runs a replay's semantic search on a snapshot held for the
// search alone.
func (a *Application) replaySearch(ctx context.Context, runtime *ProjectRuntime, text string) (result *query.SearchResult, err error) {
	selected, err := acquireSnapshotForSearch(ctx, runtime, "", "")
	if err != nil {
		return nil, err
	}
	defer selected.releaseInto(&err)
	target := SearchTarget{state: &searchTargetState{members: []*searchTargetMember{{runtime: runtime, selected: selected}}, mode: SearchSyncLocal}}
	defer target.state.closed.Store(true)
	found, _, err := a.searchTarget(ctx, target, query.SearchQuery{Phrase: model.ShortTitle(text, replayPhrase), Limit: model.ReplayLines, SyncMode: SearchSyncLocal})
	return found, err
}

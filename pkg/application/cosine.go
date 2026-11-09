package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/secrets"
)

// cosinePass is what the cosine stage did to one gather's candidates
// (20261109-004506-d-cpt-udz, 5.8 item 11).
type cosinePass struct {
	judge    string
	scores   map[string]uint8
	kept     []model.GatherCandidate
	setAside []string
}

// runCosine scores candidates by their vector similarity to subject, the
// score the semantic search ranks by, and sets aside those below the floor
// calibrated on the labelled pairs' similarities. Until the floor holds it
// only scores, so publish can store the similarities of the pairs the reader
// labels.
func (a *Application) runCosine(ctx context.Context, runtime *ProjectRuntime, selected *readSnapshotSelection, mode SearchSyncMode, ledger *model.RelevanceLedger, subject string, candidates []model.GatherCandidate, limit int) (cosinePass, error) {
	pass := cosinePass{kept: candidates}
	if subject == "" || len(candidates) == 0 || runtime.options.Embedder == nil || runtime.options.SearchIndex == nil {
		return pass, nil
	}
	err := secrets.CheckText("the draft", subject)
	if err != nil {
		return cosinePass{}, err
	}
	target := SearchTarget{state: &searchTargetState{members: []*searchTargetMember{{runtime: runtime, selected: selected}}, mode: mode}}
	defer target.state.closed.Store(true)
	found, _, err := a.searchTarget(ctx, target, query.SearchQuery{Phrase: subject, Limit: limit, SyncMode: mode})
	if err != nil {
		return cosinePass{}, fmt.Errorf("sdd: gather: cosine stage: %w", err)
	}
	pass.judge = model.CosineJudge(runtime.options.Embedder.Fingerprint())
	similarity := map[string]float32{}
	for _, hit := range found.Entries {
		if hit.Entry != nil && hit.RepoID == "" {
			similarity[hit.Entry.ID] = max(similarity[hit.Entry.ID], hit.Score)
		}
	}
	pass.scores = map[string]uint8{}
	for _, candidate := range candidates {
		score, ok := similarity[candidate.Entry.ID]
		if ok {
			pass.scores[candidate.Entry.ID] = model.ScoreByte(float64(score))
		}
	}
	pass.kept, pass.setAside = model.PlaceByCosine(candidates, pass.scores, model.CalibrateCosine(ledger.Labels(pass.judge)))
	return pass, nil
}

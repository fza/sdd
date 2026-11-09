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
)

// GroundingPolicy is a project's grounding configuration
// (20261109-004506-d-cpt-udz). A zero limit takes its default.
type GroundingPolicy struct {
	Enforce       bool
	DepthDown     int
	DepthUp       int
	BriefAllBelow int
	PrefilterFrom int
}

func (p GroundingPolicy) resolved() GroundingPolicy {
	defaults := model.GroundingConfig{}.Policy()
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

// gatherSearchLimit is how many hits one grounding search lists. Listings
// are never bounded: a topic's live population is the read (D29).
const gatherSearchLimit = 20

// GatherRequest asks for a grounding serve (20261109-004506-d-cpt-udz, 5.3).
// Draft, Kind and Topics describe the draft; IDs opens entries in full; Query
// and Terms replace the query writer with explicit searches, a path only an
// uncredited read takes.
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
	// Held reports whether the consumer already holds an entry in full; such
	// a candidate prints as a pointer (D34). Nil serves every line in full,
	// as the CLI does (R14).
	Held func(id string) bool
	// since is the corpus mark of the capture's reading; the live entries it
	// lacks are listed as landed since the reading began (D13).
	since *model.CorpusMark
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

	var reads []model.GroundingRead
	switch {
	case len(live) < policy.BriefAllBelow:
		reads = append(reads, model.GroundingRead{Mode: model.GroundingBriefs, IDs: liveIDs})
	case strings.TrimSpace(request.Draft) != "" || request.Query != "" || len(request.Terms) > 0:
		searched, err := a.gatherSearches(ctx, runtime, selected, request)
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
			"sdd: gather needs the draft: the graph holds %d live entries, at or above grounding.brief_all_below (%d), so the reading searches from the draft's text", len(live), policy.BriefAllBelow)}
	}
	landed := request.since.Since(liveIDs)
	if len(landed) > 0 {
		reads = append(reads, model.GroundingRead{Mode: model.GroundingNew, IDs: landed})
	}
	if len(opened) > 0 {
		reads = append(reads, model.GroundingRead{Mode: model.GroundingOpened, IDs: opened})
	}

	candidates, setAside := model.GatherCandidates(graph, live, reads, opened, request.Answered)
	served := &query.GatherResult{
		Graph: graph, Reads: reads, Corpus: model.NewCorpusMark(liveIDs), Prefilter: "off",
		ToJudge: candidates, SetAside: setAside, Held: map[string]bool{},
	}
	full := opened
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
		id := candidate.Entry.ID
		result.SummaryIDs = append(result.SummaryIDs, id)
		if request.Held != nil && request.Held(id) {
			served.Held[id] = true
		}
	}
	for _, e := range served.Overflow {
		result.SummaryIDs = append(result.SummaryIDs, e.ID)
	}
	var rendered bytes.Buffer
	presenters.RenderGather(&rendered, served)
	result.Text = strings.TrimRight(rendered.String(), "\n")
	result.payload = model.ReadPayload{Reads: reads, Full: slices.Clone(full), Corpus: served.Corpus}
	return result, nil
}

// gatherSearches runs the semantic search and the keyword searches. The
// embedder is checked before the query writer spends any LLM tokens (D24).
func (a *Application) gatherSearches(ctx context.Context, runtime *ProjectRuntime, selected *readSnapshotSelection, request GatherRequest) ([]model.GroundingRead, error) {
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
		q.Limit = gatherSearchLimit
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
			if hit.Entry != nil && len(ids) < gatherSearchLimit {
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

package query

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/application/types"
)

// ShownEntries is the result in its public data form, the one input of the
// show renderers and the application's structured show result
// (20261101-182333-d-tac-jiy).
func (r *ShowResult) ShownEntries() ([]types.ShownEntry, error) {
	var shown []types.ShownEntry
	for _, group := range r.Groups {
		if group.Primary == nil {
			continue
		}
		entry, err := shownEntry(group)
		if err != nil {
			return nil, err
		}
		shown = append(shown, entry)
	}
	return shown, nil
}

func shownEntry(group ShowGroup) (types.ShownEntry, error) {
	entry := group.Primary
	id := group.PrimaryID
	if id == "" {
		id = entry.ID
	}
	shown := types.ShownEntry{
		ID: id, Title: entry.Title, Type: string(entry.Type), Kind: string(entry.Kind), Layer: string(entry.Layer),
		Status: statusText(group.PrimaryStatus), Summary: entry.Summary, Body: entry.Content,
		Upstream: shownChain(group.Upstream), Downstream: shownChain(group.Downstream),
		UpstreamTruncated: truncatedIDs(group.UpstreamTruncated), DownstreamTruncated: truncatedIDs(group.DownstreamTruncated),
		Confidence: entry.Confidence, Intent: string(entry.Intent), Participants: slices.Clone(entry.Participants),
		Canonical: entry.Canonical, Aliases: slices.Clone(entry.Aliases),
		ActorKind: string(entry.ActorKind), Class: string(entry.Class), Actor: entry.Actor,
		Topics: topicLabels(group.PrimaryTopics), Refs: shownRefs(entry.Refs),
		Closes: slices.Clone(entry.Closes), Supersedes: slices.Clone(entry.Supersedes), Attachments: slices.Clone(entry.Attachments),
		Preflight: entry.Preflight, Grounding: entry.Grounding, Verification: entry.Verification,
		DerivedStatus: derivedStatus(group.PrimaryStatus, group.PrimarySupersedePath), Time: entry.Time,
	}
	if id == entry.ID {
		shown.Brief = model.EntryBrief(entry)
	}
	if entry.Index != nil {
		shown.Index = &types.ShownFactIndex{Title: entry.Index.Title, Topic: entry.Index.TopicText()}
	}
	if entry.ProcedureSpec != nil {
		procedure, err := shownProcedureSpec(entry.ProcedureSpec)
		if err != nil {
			return types.ShownEntry{}, fmt.Errorf("procedure spec of %s: %w", id, err)
		}
		shown.Procedure = procedure
	}
	return shown, nil
}

func shownProcedureSpec(spec *model.ProcedureSpecRaw) (*types.ShownProcedureSpec, error) {
	var shown types.ShownProcedureSpec
	for _, section := range []struct {
		name   string
		node   *yaml.Node
		target *string
	}{
		{"params", &spec.Params, &shown.Params},
		{"state", &spec.State, &shown.State},
		{"steps", &spec.Steps, &shown.Steps},
		{"framing", &spec.Framing, &shown.Framing},
	} {
		if section.node.IsZero() {
			continue
		}
		text, err := yaml.Marshal(section.node)
		if err != nil {
			return nil, fmt.Errorf("encoding %s: %w", section.name, err)
		}
		*section.target = string(text)
	}
	return &shown, nil
}

func shownChain(items []model.ShowTreeItem) []types.ShownChainItem {
	var shown []types.ShownChainItem
	for _, item := range items {
		shown = append(shown, shownChainItem(item))
	}
	return shown
}

func shownChainItem(item model.ShowTreeItem) types.ShownChainItem {
	shown := types.ShownChainItem{
		ID: item.NodeID(), Status: statusText(item.Status), RefKind: string(item.RefKind), Desc: item.RefDesc, Depth: item.Depth,
		SeenInResponse: item.ShownAbove || item.ShownBelow, Truncated: truncatedIDs(item.Truncated),
		Relations: slices.Clone(item.Relations), DerivedStatus: derivedStatus(item.Status, item.SupersedePath),
		ShownAbove: item.ShownAbove, ShownBelow: item.ShownBelow, TruncatedReason: item.TruncatedReason,
	}
	if len(item.Relations) > 0 {
		shown.Relation = item.Relations[0]
	}
	if entry := item.Entry; entry != nil {
		shown.Title, shown.Kind, shown.Summary = entry.Title, string(entry.Kind), entry.Summary
		shown.KindLabel, shown.FirstSentence = kindLabel(entry), entry.FirstSummarySentence()
		if item.NodeID() == entry.ID {
			shown.Brief = model.EntryBrief(entry)
		}
	} else if item.CrossRepoID != "" {
		repo, _, _ := model.SplitCrossRepoID(item.CrossRepoID)
		shown.UnresolvedRepo = string(repo)
	}
	return shown
}

// kindLabel names a kindless decision by its legacy default kind, directive,
// and a kindless signal by its type.
func kindLabel(entry *model.Entry) string {
	switch {
	case entry.Kind != "":
		return string(entry.Kind)
	case entry.Type == model.TypeDecision:
		return string(model.KindDirective)
	default:
		return entry.TypeLabel()
	}
}

// Hits is the result in its public data form, the one input of the search
// renderer and the application's structured search result. A cross-graph
// hit's status and topics derive in the member graph it came from.
func (r *SearchResult) Hits(graph *model.Graph) []types.SearchHit {
	var hits []types.SearchHit
	for _, found := range r.Entries {
		entry := found.Entry
		if entry == nil {
			continue
		}
		owner := graph
		if found.RepoID != "" {
			if member, _ := graph.MemberGraph(found.RepoID); member != nil {
				owner = member
			}
		}
		status := owner.DerivedStatus(entry)
		brief := ""
		if found.RepoID == "" {
			brief = model.EntryBrief(entry)
		}
		hits = append(hits, types.SearchHit{
			ID: found.DisplayID(), Title: entry.Title, Type: string(entry.Type), Kind: string(entry.Kind), Layer: string(entry.Layer),
			Status: statusText(status), Summary: entry.Summary, Score: found.Score,
			Confidence: entry.Confidence, Intent: string(entry.Intent), Participants: slices.Clone(entry.Participants),
			Topics: topicLabels(owner.EffectiveTopics(entry)), DerivedStatus: derivedStatus(status, nil),
			BodyExcerpt: entry.ShortContent(200), Brief: brief, Citations: shownCitations(found.Citations),
		})
	}
	return hits
}

func shownCitations(citations []Citation) []types.SearchCitation {
	var shown []types.SearchCitation
	for _, citation := range citations {
		shown = append(shown, types.SearchCitation{
			Breadcrumb: slices.Clone(citation.Breadcrumb), Snippet: citation.Snippet, AttachmentPath: citation.SourceAttachmentPath,
			IsSummary: citation.IsSummary, IsAttachment: citation.IsAttachment, Score: citation.Score,
		})
	}
	return shown
}

func statusText(status model.Status) string {
	if status.By != "" {
		return string(status.Kind) + " " + status.By
	}
	return string(status.Kind)
}

func derivedStatus(status model.Status, supersedePath []string) types.DerivedStatus {
	return types.DerivedStatus{Kind: string(status.Kind), By: status.By, SupersedePath: slices.Clone(supersedePath)}
}

func shownRefs(refs []model.Ref) []types.ShownRef {
	var shown []types.ShownRef
	for _, ref := range refs {
		shown = append(shown, types.ShownRef{ID: ref.ID, Kind: string(ref.Kind), Desc: ref.Desc})
	}
	return shown
}

func truncatedIDs(refs []model.TruncatedRef) []string {
	var ids []string
	for _, ref := range refs {
		ids = append(ids, ref.ID)
	}
	return ids
}

func topicLabels(paths []model.TopicPath) []string {
	var labels []string
	for _, path := range paths {
		labels = append(labels, path.String())
	}
	return labels
}

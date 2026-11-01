package application

import (
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// ShownEntry is one primary entry of a show, with its chains, as data
// (20260928-080143-d-cpt-kv4).
type ShownEntry struct {
	ID      string
	Title   string
	Type    string
	Kind    string
	Layer   string
	Status  string
	Summary string
	Body    string
	// Upstream and Downstream are each chain in preorder: an item is
	// followed by its descendants, Depth 1 linking to the entry itself.
	Upstream            []ShownChainItem
	Downstream          []ShownChainItem
	UpstreamTruncated   []string
	DownstreamTruncated []string
}

// ShownChainItem is one entry in a show chain. Relation is the edge
// (refs, closes, supersedes, refd-by, ...), RefKind the refs edge's kind.
// SeenInResponse marks an entry the same show already carries elsewhere.
type ShownChainItem struct {
	ID             string
	Title          string
	Kind           string
	Status         string
	Summary        string
	Relation       string
	RefKind        string
	Desc           string
	Depth          int
	SeenInResponse bool
	Truncated      []string
}

// SearchHit is one search match as data.
type SearchHit struct {
	ID      string
	Title   string
	Type    string
	Kind    string
	Layer   string
	Status  string
	Summary string
	Score   float32
}

func statusText(status model.Status) string {
	if status.By != "" {
		return string(status.Kind) + " " + status.By
	}
	return string(status.Kind)
}

func shownEntries(result *query.ShowResult) []ShownEntry {
	var shown []ShownEntry
	for _, group := range result.Groups {
		if group.Primary == nil {
			continue
		}
		id := group.PrimaryID
		if id == "" {
			id = group.Primary.ID
		}
		entry := group.Primary
		shown = append(shown, ShownEntry{
			ID: id, Title: entry.Title, Type: string(entry.Type), Kind: string(entry.Kind), Layer: string(entry.Layer),
			Status: statusText(group.PrimaryStatus), Summary: entry.Summary, Body: entry.Content,
			Upstream: chain(group.Upstream), Downstream: chain(group.Downstream),
			UpstreamTruncated: truncatedIDs(group.UpstreamTruncated), DownstreamTruncated: truncatedIDs(group.DownstreamTruncated),
		})
	}
	return shown
}

func chain(items []model.ShowTreeItem) []ShownChainItem {
	var shown []ShownChainItem
	for _, item := range items {
		shown = append(shown, chainItem(item))
	}
	return shown
}

func chainItem(item model.ShowTreeItem) ShownChainItem {
	shown := ShownChainItem{
		ID: item.NodeID(), Status: statusText(item.Status), RefKind: string(item.RefKind), Desc: item.RefDesc, Depth: item.Depth,
		SeenInResponse: item.ShownAbove || item.ShownBelow, Truncated: truncatedIDs(item.Truncated),
	}
	if len(item.Relations) > 0 {
		shown.Relation = item.Relations[0]
	}
	if item.Entry != nil {
		shown.Title, shown.Kind, shown.Summary = item.Entry.Title, string(item.Entry.Kind), item.Entry.Summary
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

func searchHits(result *query.SearchResult, graph *model.Graph) []SearchHit {
	var hits []SearchHit
	for _, found := range result.Entries {
		if found.Entry == nil {
			continue
		}
		owner := graph
		if found.RepoID != "" {
			if member, _ := graph.MemberGraph(found.RepoID); member != nil {
				owner = member
			}
		}
		entry := found.Entry
		hits = append(hits, SearchHit{
			ID: found.DisplayID(), Title: entry.Title, Type: string(entry.Type), Kind: string(entry.Kind), Layer: string(entry.Layer),
			Status: statusText(owner.DerivedStatus(entry)), Summary: entry.Summary, Score: found.Score,
		})
	}
	return hits
}

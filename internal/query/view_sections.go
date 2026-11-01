package query

import (
	"slices"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/application/types"
)

// ViewSections is the result in its public data form, the one input of the
// view renderer and the application's structured view result
// (20261101-194502-d-tac-r3f). repo tags each section with the connected repo
// whose graph produced it, empty for the local graph.
func (r *ViewResult) ViewSections(repo model.RepoID) []types.ViewSection {
	sections := make([]types.ViewSection, 0, len(r.Sections))
	for _, section := range r.Sections {
		shown := types.ViewSection{Render: section.Render, Name: section.Name, Brief: section.Brief, Repo: string(repo)}
		if section.Data != nil {
			shown.Matched = section.Data.Count()
		}
		switch data := section.Data.(type) {
		case model.FlatList:
			shown.List = viewList(r.Graph, data)
		case model.Grouped:
			shown.Grouped = viewGrouped(r.Graph, data)
		case model.Counts:
			shown.Counts = viewCounts(data)
		case model.FocusBlock:
			shown.Focus = viewFocusBlock(r.Graph, data)
		case model.ParticipantsBlock:
			shown.Participants = viewParticipantsBlock(r.Graph, data)
		case model.WipList:
			shown.Wip = viewWipList(data)
		case model.Bodies:
			shown.Bodies = viewBodies(r.Graph, data)
		}
		sections = append(sections, shown)
	}
	return sections
}

// listedEntry is the entry as a listing line shows it, its status and
// topics derived in g.
func listedEntry(e *model.Entry, g *model.Graph) types.ListedEntry {
	status := g.DerivedStatus(e)
	return types.ListedEntry{
		ID: e.ID, Title: e.Title, ListingTitle: e.ListingTitle(), Type: string(e.Type), Kind: string(e.Kind), Layer: string(e.Layer),
		Status: statusText(status), DerivedStatus: derivedStatus(status, nil), Confidence: e.Confidence, Intent: string(e.Intent),
		Participants: slices.Clone(e.Participants), Topics: topicLabels(g.EffectiveTopics(e)), Summary: e.Summary,
		BodyExcerpt: e.ShortContent(200), Canonical: e.Canonical,
	}
}

func listedEntries(g *model.Graph, entries []*model.Entry) []types.ListedEntry {
	var listed []types.ListedEntry
	for _, e := range entries {
		listed = append(listed, listedEntry(e, g))
	}
	return listed
}

func listedEntryRef(g *model.Graph, e *model.Entry) *types.ListedEntry {
	if e == nil {
		return nil
	}
	listed := listedEntry(e, g)
	return &listed
}

// viewList keeps the model's alignment rule: scores and ref expansions count
// only when parallel to the entries.
func viewList(g *model.Graph, flat model.FlatList) *types.ViewList {
	list := &types.ViewList{
		Ranked:       len(flat.Scores) == len(flat.Entries) && len(flat.Scores) > 0,
		RefsExpanded: len(flat.RefExpansions) == len(flat.Entries),
	}
	dropped := len(flat.RefExpansionDropped) == len(flat.Entries)
	for i, e := range flat.Entries {
		item := types.ViewListItem{Entry: listedEntry(e, g)}
		if list.Ranked {
			item.Score = flat.Scores[i]
		}
		if list.RefsExpanded {
			item.Refs = viewRefs(flat.RefExpansions[i])
			if dropped {
				item.RefsDropped = flat.RefExpansionDropped[i]
			}
		}
		list.Items = append(list.Items, item)
	}
	return list
}

func viewRefs(expansions []model.RefExpansion) []types.ViewRef {
	var refs []types.ViewRef
	for _, ref := range expansions {
		refs = append(refs, types.ViewRef{
			Kind: string(ref.Kind), ID: ref.ID, DerivedStatus: derivedStatus(ref.Status, ref.SupersedePath),
			Desc: ref.Desc, UnresolvedRepo: string(ref.UnresolvedRepo),
		})
	}
	return refs
}

func viewGrouped(g *model.Graph, grouped model.Grouped) *types.ViewGrouped {
	shown := &types.ViewGrouped{Field: grouped.Field}
	for _, group := range grouped.Groups {
		shown.Groups = append(shown.Groups, types.ViewGroup{Key: group.Key, Entries: listedEntries(g, group.Entries)})
	}
	return shown
}

func viewCounts(counts model.Counts) *types.ViewCounts {
	shown := &types.ViewCounts{}
	for _, row := range counts.Rows {
		shown.Rows = append(shown.Rows, types.ViewCountRow{Label: row.Label, Count: row.Count, Heat: row.Heat})
	}
	return shown
}

// viewFocusBlock drops targets whose entry did not resolve; the renderer
// shows none of them.
func viewFocusBlock(g *model.Graph, block model.FocusBlock) *types.ViewFocusBlock {
	shown := &types.ViewFocusBlock{Dropped: block.Dropped, Pull: block.Pull}
	for _, group := range block.Focuses {
		focus := types.ViewFocus{Focus: listedEntryRef(g, group.Focus), Actors: slices.Clone(group.Actors), When: cloneWhen(group.When)}
		for _, target := range group.Targets {
			if target.Target == nil {
				continue
			}
			focus.Targets = append(focus.Targets, types.ViewFocusTarget{
				Target: listedEntry(target.Target, g), Actors: slices.Clone(target.ResolvedActors), ActorsExplicit: target.ActorsExplicit,
				When: cloneWhen(target.ResolvedWhen), Score: target.Score, State: string(target.State),
			})
		}
		shown.Focuses = append(shown.Focuses, focus)
	}
	return shown
}

func cloneWhen(when *model.FocusWhen) *types.FocusWhen {
	if when == nil {
		return nil
	}
	clone := *when
	return &clone
}

func viewParticipantsBlock(g *model.Graph, block model.ParticipantsBlock) *types.ViewParticipantsBlock {
	shown := &types.ViewParticipantsBlock{Dropped: block.Dropped, Pull: block.Pull}
	for _, group := range block.Groups {
		shown.Groups = append(shown.Groups, types.ViewParticipantsGroup{Actor: listedEntryRef(g, group.Actor), Roles: listedEntries(g, group.Roles)})
	}
	return shown
}

func viewWipList(list model.WipList) *types.ViewWipList {
	shown := &types.ViewWipList{Dropped: list.Dropped, Pull: list.Pull}
	for _, marker := range list.Markers {
		shown.Markers = append(shown.Markers, types.ViewWipMarker{
			ID: marker.ID, Entry: marker.Entry, Participant: marker.Participant, Exclusive: marker.Exclusive, Branch: marker.Branch,
			Content: marker.Content, ContentExcerpt: marker.ShortContent(200), Time: marker.Time,
		})
	}
	return shown
}

func viewBodies(g *model.Graph, bodies model.Bodies) *types.ViewBodies {
	shown := &types.ViewBodies{Dropped: bodies.Dropped, Pull: bodies.Pull}
	for _, e := range bodies.Entries {
		listed := listedEntry(e, g)
		listed.Body = e.Content
		shown.Entries = append(shown.Entries, listed)
	}
	return shown
}

package mcpapp

import (
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// View object results join the token_saving object rule
// (20261101-194502-d-tac-r3f): one object per section, exactly one shape
// field filled.

type ViewSectionResult struct {
	Render       string                       `json:"render" jsonschema:"the render terminating the section: as-list, as-grouped, as-counts, as-focus-block, as-participants-block, as-wip-list or as-bodies"`
	Name         string                       `json:"name,omitempty" jsonschema:"section header set by name(...)"`
	Brief        bool                         `json:"brief,omitempty" jsonschema:"the layout asked for compact entry lines"`
	Repo         string                       `json:"repo,omitempty" jsonschema:"connected repo whose graph produced the section; empty for the local graph"`
	Matched      int                          `json:"matched" jsonschema:"primary units the section produced: entries, focuses, actors or WIP markers; for counts, the entries aggregated. 0 means it matched nothing"`
	List         *ViewListResult              `json:"list,omitempty"`
	Grouped      *ViewGroupedResult           `json:"grouped,omitempty"`
	Counts       *ViewCountsResult            `json:"counts,omitempty"`
	Focus        *ViewFocusBlockResult        `json:"focus,omitempty"`
	Participants *ViewParticipantsBlockResult `json:"participants,omitempty"`
	Wip          *ViewWipListResult           `json:"wip,omitempty"`
	Bodies       *ViewBodiesResult            `json:"bodies,omitempty"`
}

type ListedEntryResult struct {
	ID           string   `json:"id"`
	Title        string   `json:"title,omitempty"`
	Type         string   `json:"type"`
	Kind         string   `json:"kind,omitempty"`
	Layer        string   `json:"layer"`
	Status       string   `json:"status,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Confidence   string   `json:"confidence,omitempty"`
	Intent       string   `json:"intent,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Topics       []string `json:"topics,omitempty" jsonschema:"effective topics: the entry's own merged with the topic annotations naming it"`
	Canonical    string   `json:"canonical,omitempty" jsonschema:"an actor's canonical name"`
	Body         string   `json:"body,omitempty" jsonschema:"the entry's full text, set only in a bodies section"`
}

type ViewListResult struct {
	Items        []ViewListItemResult `json:"items"`
	Ranked       bool                 `json:"ranked,omitempty" jsonschema:"the section was ranked: every item carries its score"`
	RefsExpanded bool                 `json:"refs_expanded,omitempty" jsonschema:"the section used expand(refs): every item carries its resolved outgoing refs"`
}

type ViewListItemResult struct {
	Entry       ListedEntryResult `json:"entry"`
	Score       float64           `json:"score,omitempty"`
	Refs        []ViewRefResult   `json:"refs,omitempty"`
	RefsDropped int               `json:"refs_dropped,omitempty" jsonschema:"refs a serve budget kept off this item"`
}

type ViewRefResult struct {
	Ref            string `json:"ref" jsonschema:"kind of the refs edge; unknown for a legacy bare-string ref"`
	ID             string `json:"id"`
	Status         string `json:"status,omitempty" jsonschema:"the target's derived status; empty for a done signal or a dangling target"`
	Desc           string `json:"desc,omitempty"`
	UnresolvedRepo string `json:"unresolved_repo,omitempty" jsonschema:"connected repo of a cross-repo target whose graph is not available locally"`
}

type ViewGroupedResult struct {
	Field  string            `json:"field" jsonschema:"the entry attribute the groups share a value of"`
	Groups []ViewGroupResult `json:"groups"`
}

type ViewGroupResult struct {
	Key     string              `json:"key"`
	Entries []ListedEntryResult `json:"entries"`
}

type ViewCountsResult struct {
	Rows []ViewCountRowResult `json:"rows"`
}

type ViewCountRowResult struct {
	Label string  `json:"label"`
	Count int     `json:"count"`
	Heat  float64 `json:"heat"`
}

type ViewFocusBlockResult struct {
	Focuses []ViewFocusResult `json:"focuses"`
	Dropped int               `json:"dropped,omitempty" jsonschema:"focuses a serve budget kept out of the section"`
	Pull    string            `json:"pull,omitempty" jsonschema:"layout serving the complete section"`
}

type ViewFocusResult struct {
	Focus   *ListedEntryResult      `json:"focus,omitempty" jsonschema:"the focus decision; absent when it is missing from the graph"`
	Actors  []string                `json:"actors,omitempty" jsonschema:"focus-level default actors"`
	When    *FocusWhenResult        `json:"when,omitempty" jsonschema:"focus-level default temporal scope"`
	Targets []ViewFocusTargetResult `json:"targets"`
}

type ViewFocusTargetResult struct {
	Target         ListedEntryResult `json:"target"`
	Actors         []string          `json:"actors,omitempty" jsonschema:"resolved actors: the involvement's own, else the focus defaults; empty means pull-available"`
	ActorsExplicit bool              `json:"actors_explicit,omitempty" jsonschema:"the involvement declared its actors"`
	When           *FocusWhenResult  `json:"when,omitempty"`
	Score          float64           `json:"score"`
	State          string            `json:"state" jsonschema:"pull-available, stalled or driving"`
}

type FocusWhenResult struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type ViewParticipantsBlockResult struct {
	Groups  []ViewParticipantsGroupResult `json:"groups"`
	Dropped int                           `json:"dropped,omitempty" jsonschema:"actors a serve budget kept out of the section"`
	Pull    string                        `json:"pull,omitempty" jsonschema:"layout serving the complete section"`
}

type ViewParticipantsGroupResult struct {
	Actor *ListedEntryResult  `json:"actor,omitempty" jsonschema:"the active actor; absent when it is missing from the graph"`
	Roles []ListedEntryResult `json:"roles"`
}

type ViewWipListResult struct {
	Markers []ViewWipMarkerResult `json:"markers"`
	Dropped int                   `json:"dropped,omitempty" jsonschema:"markers a serve budget kept out of the section"`
	Pull    string                `json:"pull,omitempty" jsonschema:"layout serving the complete section"`
}

type ViewWipMarkerResult struct {
	ID          string `json:"id"`
	Entry       string `json:"entry" jsonschema:"ID of the entry being worked on"`
	Participant string `json:"participant"`
	Exclusive   bool   `json:"exclusive,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Content     string `json:"content"`
	Time        string `json:"time,omitempty" jsonschema:"when the marker was set, RFC 3339"`
}

type ViewBodiesResult struct {
	Entries []ListedEntryResult `json:"entries"`
	Dropped int                 `json:"dropped,omitempty" jsonschema:"entries a serve budget kept out of the section"`
	Pull    string              `json:"pull,omitempty" jsonschema:"layout serving the complete section"`
}

func viewSectionItems(sections []sdd.ViewSection) []ViewSectionResult {
	items := make([]ViewSectionResult, 0, len(sections))
	for _, section := range sections {
		item := ViewSectionResult{Render: section.Render, Name: section.Name, Brief: section.Brief, Repo: section.Repo, Matched: section.Matched}
		if list := section.List; list != nil {
			item.List = &ViewListResult{Items: make([]ViewListItemResult, 0, len(list.Items)), Ranked: list.Ranked, RefsExpanded: list.RefsExpanded}
			for _, listed := range list.Items {
				item.List.Items = append(item.List.Items, ViewListItemResult{
					Entry: listedEntryItem(listed.Entry), Score: listed.Score, Refs: viewRefItems(listed.Refs), RefsDropped: listed.RefsDropped,
				})
			}
		}
		if grouped := section.Grouped; grouped != nil {
			item.Grouped = &ViewGroupedResult{Field: grouped.Field, Groups: make([]ViewGroupResult, 0, len(grouped.Groups))}
			for _, group := range grouped.Groups {
				item.Grouped.Groups = append(item.Grouped.Groups, ViewGroupResult{Key: group.Key, Entries: listedEntryItems(group.Entries)})
			}
		}
		if counts := section.Counts; counts != nil {
			item.Counts = &ViewCountsResult{Rows: make([]ViewCountRowResult, 0, len(counts.Rows))}
			for _, row := range counts.Rows {
				item.Counts.Rows = append(item.Counts.Rows, ViewCountRowResult{Label: row.Label, Count: row.Count, Heat: row.Heat})
			}
		}
		if block := section.Focus; block != nil {
			item.Focus = &ViewFocusBlockResult{Focuses: make([]ViewFocusResult, 0, len(block.Focuses)), Dropped: block.Dropped, Pull: block.Pull}
			for _, focus := range block.Focuses {
				shown := ViewFocusResult{Focus: listedEntryPointer(focus.Focus), Actors: focus.Actors, When: focusWhenItem(focus.When), Targets: make([]ViewFocusTargetResult, 0, len(focus.Targets))}
				for _, target := range focus.Targets {
					shown.Targets = append(shown.Targets, ViewFocusTargetResult{
						Target: listedEntryItem(target.Target), Actors: target.Actors, ActorsExplicit: target.ActorsExplicit,
						When: focusWhenItem(target.When), Score: target.Score, State: target.State,
					})
				}
				item.Focus.Focuses = append(item.Focus.Focuses, shown)
			}
		}
		if block := section.Participants; block != nil {
			item.Participants = &ViewParticipantsBlockResult{Groups: make([]ViewParticipantsGroupResult, 0, len(block.Groups)), Dropped: block.Dropped, Pull: block.Pull}
			for _, group := range block.Groups {
				item.Participants.Groups = append(item.Participants.Groups, ViewParticipantsGroupResult{Actor: listedEntryPointer(group.Actor), Roles: listedEntryItems(group.Roles)})
			}
		}
		if list := section.Wip; list != nil {
			item.Wip = &ViewWipListResult{Markers: make([]ViewWipMarkerResult, 0, len(list.Markers)), Dropped: list.Dropped, Pull: list.Pull}
			for _, marker := range list.Markers {
				shown := ViewWipMarkerResult{ID: marker.ID, Entry: marker.Entry, Participant: marker.Participant, Exclusive: marker.Exclusive, Branch: marker.Branch, Content: marker.Content}
				if !marker.Time.IsZero() {
					shown.Time = marker.Time.Format(time.RFC3339)
				}
				item.Wip.Markers = append(item.Wip.Markers, shown)
			}
		}
		if bodies := section.Bodies; bodies != nil {
			item.Bodies = &ViewBodiesResult{Entries: listedEntryItems(bodies.Entries), Dropped: bodies.Dropped, Pull: bodies.Pull}
		}
		items = append(items, item)
	}
	return items
}

func listedEntryItem(entry types.ListedEntry) ListedEntryResult {
	return ListedEntryResult{
		ID: entry.ID, Title: entry.Title, Type: entry.Type, Kind: entry.Kind, Layer: entry.Layer, Status: entry.Status, Summary: entry.Summary,
		Confidence: entry.Confidence, Intent: entry.Intent, Participants: entry.Participants, Topics: entry.Topics, Canonical: entry.Canonical, Body: entry.Body,
	}
}

func listedEntryItems(entries []types.ListedEntry) []ListedEntryResult {
	items := make([]ListedEntryResult, 0, len(entries))
	for _, entry := range entries {
		items = append(items, listedEntryItem(entry))
	}
	return items
}

func listedEntryPointer(entry *types.ListedEntry) *ListedEntryResult {
	if entry == nil {
		return nil
	}
	item := listedEntryItem(*entry)
	return &item
}

func viewRefItems(refs []types.ViewRef) []ViewRefResult {
	var items []ViewRefResult
	for _, ref := range refs {
		status := ref.DerivedStatus.Kind
		if ref.DerivedStatus.By != "" {
			status += " " + ref.DerivedStatus.By
		}
		items = append(items, ViewRefResult{Ref: ref.Kind, ID: ref.ID, Status: status, Desc: ref.Desc, UnresolvedRepo: ref.UnresolvedRepo})
	}
	return items
}

func focusWhenItem(when *types.FocusWhen) *FocusWhenResult {
	if when.IsZero() {
		return nil
	}
	return &FocusWhenResult{From: when.From, To: when.To}
}

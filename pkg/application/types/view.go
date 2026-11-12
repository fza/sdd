package types

import (
	"fmt"
	"time"
)

// ViewSection is one section of a view as data (20261101-194502-d-tac-r3f):
// the render that terminates it, its header, and exactly one shape filled,
// the one Render names.
type ViewSection struct {
	// Render names the render function terminating the section's pipeline
	// (as-list, as-grouped, as-counts, as-focus-block, as-participants-block,
	// as-wip-list, as-bodies).
	Render string
	// Name is the section header set by name(...); empty renders no header.
	Name string
	// Brief selects the compact entry line: identity and listing title only.
	Brief bool
	// Repo names the connected repo whose graph produced the section, empty
	// for the local graph.
	Repo string
	// Matched is how many primary units the section produced: entries,
	// focuses, actors or WIP markers by shape; for counts, the entries
	// aggregated. Zero means the section matched nothing.
	Matched int

	List         *ViewList
	Grouped      *ViewGrouped
	Counts       *ViewCounts
	Focus        *ViewFocusBlock
	Participants *ViewParticipantsBlock
	Wip          *ViewWipList
	Bodies       *ViewBodies
}

// ListedEntry is an entry as a listing line shows it, the one entry form every
// view shape uses.
type ListedEntry struct {
	ID    string
	Title string
	// ListingTitle is the title, or the first summary sentence for an entry
	// that has none: what the brief line shows.
	ListingTitle  string
	Type          string
	Kind          string
	Layer         string
	Status        string
	DerivedStatus DerivedStatus
	Confidence    string
	Intent        string
	Participants  []string
	// Topics is the effective topic set: the entry's own topics merged with
	// the topic annotations naming it.
	Topics  []string
	Summary string
	// BodyExcerpt is the body's first line, cut at a sentence boundary near
	// 200 characters: what a listing shows for an entry with no summary.
	BodyExcerpt string
	// Canonical is an actor's canonical name.
	Canonical string
	// Brief is the pointer text the no-repeat rule prints for an entry the
	// consumer already holds in full (20261109-004506-d-cpt-udz, R14).
	Brief string
	// Body is the entry's full text, filled only in a bodies section.
	Body string
}

// ViewList is an as-list section: entries in section order.
type ViewList struct {
	Items []ViewListItem
	// Ranked marks a section ranked by score: every item carries its Score.
	Ranked bool
	// RefsExpanded marks a section that used expand(refs): every item carries
	// its resolved outgoing refs.
	RefsExpanded bool
	// More counts the entries the section matched past Items, left out by its
	// n(k) or a size bound; Paging composes the layout serving them.
	More   int
	Paging ViewPaging
}

// ViewPaging pages a list section: Head is the section's layout with its own
// skip, n and name taken out, Offset the skip it applied.
type ViewPaging struct {
	Head   string
	Offset int
}

// Layout is the layout serving n entries of the section's own output, from
// its skip-th on.
func (p ViewPaging) Layout(skip, n int) string {
	return fmt.Sprintf("skip(%d):n(%d):%s", p.Offset+skip, n, p.Head)
}

// DropLast moves the list's last item into More, reporting whether there was
// one.
func (l *ViewList) DropLast() bool {
	if len(l.Items) == 0 {
		return false
	}
	l.Items = l.Items[:len(l.Items)-1]
	l.More++
	return true
}

// ViewListItem is one entry of an as-list section. RefsDropped counts the
// refs a serve budget kept off the item.
type ViewListItem struct {
	Entry       ListedEntry
	Score       float64
	Refs        []ViewRef
	RefsDropped int
}

// ViewRef is one resolved outgoing ref of an expand(refs) item. Kind is the
// ref's stored kind, "unknown" for a legacy bare-string ref. A dangling
// target carries a zero DerivedStatus; UnresolvedRepo names the connected
// repo of a cross-repo target whose graph is not available locally.
type ViewRef struct {
	Kind           string
	ID             string
	DerivedStatus  DerivedStatus
	Desc           string
	UnresolvedRepo string
}

// ViewGrouped is an as-grouped section: entries bucketed by Field.
type ViewGrouped struct {
	Field  string
	Groups []ViewGroup
}

// ViewGroup is one bucket of an as-grouped section, Key the field value its
// entries share.
type ViewGroup struct {
	Key     string
	Entries []ListedEntry
}

// ViewCounts is an as-counts section: one row per effective topic, busiest
// first.
type ViewCounts struct {
	Rows []ViewCountRow
}

// ViewCountRow is one topic of an as-counts section: how many entries carry
// it and their summed heat.
type ViewCountRow struct {
	Label string
	Count int
	Heat  float64
}

// ViewFocusBlock is an as-focus-block section: one focus per active focus
// decision. Dropped counts whole focuses a serve budget kept out; Pull is the
// layout expression serving the complete section.
type ViewFocusBlock struct {
	Focuses []ViewFocus
	Dropped int
	Pull    string
}

// ViewFocus is one focus decision with its involvement targets. Actors and
// When are the focus-level defaults; Focus is nil for a focus missing from
// the graph.
type ViewFocus struct {
	Focus   *ListedEntry
	Actors  []string
	When    *FocusWhen
	Targets []ViewFocusTarget
}

// ViewFocusTarget is one involvement target of a focus. Actors and When are
// resolved: the involvement's own, else the focus defaults, ActorsExplicit
// marking actors the involvement declared. Score is the target's heat under
// the section's rank; State is pull-available, stalled or driving.
type ViewFocusTarget struct {
	Target         ListedEntry
	Actors         []string
	ActorsExplicit bool
	When           *FocusWhen
	Score          float64
	State          string
}

// ViewParticipantsBlock is an as-participants-block section: one group per
// active actor. Dropped and Pull are as in ViewFocusBlock.
type ViewParticipantsBlock struct {
	Groups  []ViewParticipantsGroup
	Dropped int
	Pull    string
}

// ViewParticipantsGroup is one active actor with the active roles bound to
// its chain. Actor is nil for an actor missing from the graph.
type ViewParticipantsGroup struct {
	Actor *ListedEntry
	Roles []ListedEntry
}

// ViewWipList is an as-wip-list section: the active WIP markers. Dropped and
// Pull are as in ViewFocusBlock.
type ViewWipList struct {
	Markers []ViewWipMarker
	Dropped int
	Pull    string
}

// ViewWipMarker is one active WIP marker. ContentExcerpt is the content's
// first line, cut near 200 characters.
type ViewWipMarker struct {
	ID             string
	Entry          string
	Participant    string
	Exclusive      bool
	Branch         string
	Content        string
	ContentExcerpt string
	Time           time.Time
}

// ViewBodies is an as-bodies section: entries with their full bodies, in
// section order. Dropped and Pull are as in ViewFocusBlock; Withheld names the
// dropped entries, without bodies, since a body is cut only whole (d-tac-rzi).
type ViewBodies struct {
	Entries  []ListedEntry
	Withheld []ListedEntry
	Dropped  int
	Pull     string
}

package types

import "time"

// ShownEntry is one primary entry of a show, with its chains, as data
// (20260928-080143-d-cpt-kv4). It carries every field the show envelope
// prints, so the show renderers need nothing beside it.
type ShownEntry struct {
	// ID is the display identity: repo-prefixed for an entry of a connected
	// repo's graph.
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

	Confidence   string
	Intent       string
	Participants []string
	Canonical    string
	Aliases      []string
	ActorKind    string
	Class        string
	Actor        string
	// Topics is the effective topic set: the entry's own topics merged with
	// the topic annotations naming it.
	Topics        []string
	Index         *ShownFactIndex
	Refs          []ShownRef
	Closes        []string
	Supersedes    []string
	Attachments   []string
	Preflight     string
	DerivedStatus DerivedStatus
	Time          time.Time
	Procedure     *ShownProcedureSpec
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

	// Relations holds every edge linking the item to its parent, Relation
	// being the first.
	Relations []string
	// KindLabel is the kind a listing shows: the entry's kind, directive for
	// a kindless decision, else its type. Empty for an unresolved item.
	KindLabel     string
	FirstSentence string
	DerivedStatus DerivedStatus
	// ShownAbove marks an entry an earlier part of the same show carries;
	// ShownBelow one a later primary of it carries.
	ShownAbove bool
	ShownBelow bool
	// TruncatedReason names the frontier that cut Truncated when it is not
	// the depth limit ("fan-out cap", "chain budget").
	TruncatedReason string
	// UnresolvedRepo names the connected repo of a cross-repo item whose
	// graph is not available locally.
	UnresolvedRepo string
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

	Confidence    string
	Intent        string
	Participants  []string
	Topics        []string
	DerivedStatus DerivedStatus
	// BodyExcerpt is the body's first line, cut at a sentence boundary near
	// 200 characters: what a listing shows for an entry with no summary.
	BodyExcerpt string
	// Citations are the matching chunks, best first.
	Citations []SearchCitation
}

// SearchCitation is one chunk that matched a search. Breadcrumb is its
// heading chain, empty for a summary chunk or body text before any heading.
type SearchCitation struct {
	Breadcrumb     []string
	Snippet        string
	AttachmentPath string
	IsSummary      bool
	IsAttachment   bool
	Score          float32
}

// DerivedStatus is an entry's lifecycle status derived from the graph. Kind is
// empty for a done signal. By names the live head closer or superseder;
// SupersedePath is a superseded entry's trail from the entry itself to that
// head.
type DerivedStatus struct {
	Kind          string
	By            string
	SupersedePath []string
}

// ShownRef is one entry of an entry's refs.
type ShownRef struct {
	ID   string
	Kind string
	Desc string
}

// ShownFactIndex is a fact's enrollment in the fact index.
type ShownFactIndex struct {
	Title string
	Topic string
}

// ShownProcedureSpec is a procedure's machine part, each section as the YAML
// text of its frontmatter value. An absent section is empty.
type ShownProcedureSpec struct {
	Params  string
	State   string
	Steps   string
	Framing string
}

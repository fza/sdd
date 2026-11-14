package application

import (
	"time"

	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/pkg/application/types"
)

// Default show expansion depths favor grounding context while keeping the
// typically wider consumer side shallow.
const (
	DefaultShowUpDepth   = query.DefaultUpDepth
	DefaultShowDownDepth = query.DefaultDownDepth
)

// InfoRequest selects current read authority when Branch is empty.
// A selected branch that cannot be acquired returns an error.
type InfoRequest struct {
	Branch            string
	BranchFromSession bool
}

// InfoResult describes a project as a session reads it. GraphSchema is the
// schema the read graph records and BinarySchema the one this build writes by
// default; writes keep GraphSchema when it is older.
type InfoResult struct {
	Project      ProjectRef
	Participant  string
	Language     string
	Search       string
	GraphSchema  int
	BinarySchema int
	// Contribution is the read checkout's contribution ref, empty outside
	// contribution mode.
	Contribution string
	// SyncBlocked is what blocks automatic sync of the branch's graph
	// repository, nil while sync proceeds.
	SyncBlocked *SyncBlocked
	// SecretRules names the credential rule file this process scans with.
	SecretRules SecretRules
}

// SecretRules is the gitleaks rule file credential scans use: the built-in
// copy or one the daily pass fetched, and how fetching went.
type SecretRules struct {
	// Source is "built-in" or "fetched"; Tag is its gitleaks release.
	Source string
	Tag    string
	// Update is the effective secrets.rules_update: daily or off.
	Update string
	// Unused says why a fetched copy on disk is not in use.
	Unused        string
	LastAccepted  time.Time
	LastChecked   time.Time
	LastFailure   string
	LastFailureAt time.Time
}

type ViewRequest struct {
	Layout            string
	Branch            string
	BranchFromSession bool
	Repos             []RepoID
	AllRepos          bool
	// Budget bounds the view's scaling parts on the serve path; the zero
	// value is unbounded — explicit pulls arrive complete (d-tac-rzi).
	Budget types.ViewBudget
}

type ViewResult struct {
	Project ProjectRef
	// ViewSections are the layout's sections as data, the local graph's
	// first, then each queried dependency's, tagged with its repo.
	ViewSections []ViewSection
	// MatchedCount is the total primary units the layout produced across the
	// local graph and any queried dependency repos. Zero means the pipeline
	// matched nothing — surfaces distinguish an empty result from a failure
	// (an agent over MCP cannot tell a blank string from a broken call).
	MatchedCount int
	// KnownParticipants names the graph's canonical participants, populated
	// only when the result was empty and the layout carried a participant
	// filter — so an empty participant() view can say what names exist rather
	// than leaving an exact-match miss indistinguishable from no data.
	KnownParticipants []string
	// FullIDs and SummaryIDs name the local entries the sections served:
	// bodies in full, entry rows as summary lines.
	FullIDs    []string
	SummaryIDs []string
	// bodiesOnly marks a view whose every section serves as-bodies: its
	// budget already cut it at whole bodies.
	bodiesOnly bool
}

type ShowRequest struct {
	IDs               []string
	UpDepth           int
	DownDepth         int
	Branch            string
	BranchFromSession bool
	// Budget bounds each direction's chain expansion on the serve path; the
	// zero value is unbounded — explicit pulls arrive complete (d-tac-rzi).
	Budget types.ShowTreeBudget
}

type ShowResult struct {
	Project    ProjectRef
	Items      []ShownEntry
	FullIDs    []string
	SummaryIDs []string
}

type SearchRequest struct {
	// IncludesRevision asks the source to observe a revision containing this write.
	IncludesRevision string
	// SyncMode is required, including for text-only requests.
	SyncMode          SearchSyncMode
	Terms             []string
	Phrase            string
	Branch            string
	BranchFromSession bool
	Type              string
	Layer             string
	Kind              string
	IncludeSuperseded bool
	Limit             int
	MaxCitations      int
	Repos             []RepoID
	AllRepos          bool
}

type SearchResult struct {
	Coverage []SearchCoverage
	Notice   string
	// Notices are the notices Notice joins, each on its own: a credential
	// found in the phrase, incomplete indexing.
	Notices  []string
	Project  ProjectRef
	Hits     []SearchHit
	EntryIDs []string
}

type ReadAttachmentRequest struct {
	Branch            string
	BranchFromSession bool
	EntryID           string
	Filename          string
	Offset            int64
	MaxBytes          int
}

type ReadAttachmentResult struct {
	Project   ProjectRef
	Page      AttachmentPage
	Available []string
}

type ProcedureListRequest struct{}

type ProcedureListResult struct {
	Project    ProjectRef
	Procedures string
}

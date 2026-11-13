package types

import (
	"fmt"
	"time"
)

// UnavailableRefKind names why a ref a direct dependency holds into a
// project beyond the home's direct dependencies stays unresolved
// (20261113-235859-d-cpt-l3m).
type UnavailableRefKind string

const (
	// RefNotConnected: the project is not connected on this machine.
	RefNotConnected UnavailableRefKind = "not-connected"
	// RefMissingAfterFetch: a completed fetch of the project still lacks the
	// entry.
	RefMissingAfterFetch UnavailableRefKind = "missing-after-fetch"
	// RefFetchDeferred: the git gate deferred the fetch and no credit paid
	// for it.
	RefFetchDeferred UnavailableRefKind = "fetch-deferred"
)

// UnavailableRef is one such unresolved ref, the typed warning every surface
// reports and the reason its unavailable marker shows. At is when the miss
// was recorded for RefMissingAfterFetch and when the fetch may run next for
// RefFetchDeferred.
type UnavailableRef struct {
	Repo    RepoID
	EntryID string
	Kind    UnavailableRefKind
	At      time.Time
}

func (u *UnavailableRef) Error() string {
	return fmt.Sprintf("%s:%s unavailable: %s", u.Repo, u.EntryID, u.Reason())
}

// Reason is the short reason the unavailable marker carries.
func (u UnavailableRef) Reason() string {
	switch u.Kind {
	case RefNotConnected:
		return "not connected on this machine"
	case RefMissingAfterFetch:
		return fmt.Sprintf("not in %s as of %s", u.Repo, u.At.Local().Format(time.DateTime))
	case RefFetchDeferred:
		return "fetch deferred until " + u.At.Local().Format(time.DateTime)
	}
	return string(u.Kind)
}

// Actor names who must act for the ref to resolve.
func (u UnavailableRef) Actor() string {
	switch u.Kind {
	case RefNotConnected:
		return fmt.Sprintf("you: connect %s with `sdd repo add <clone-url>`", u.Repo)
	case RefMissingAfterFetch:
		return fmt.Sprintf("the maintainers of %s, or of the dependency holding the ref", u.Repo)
	}
	return "nobody: sdd fetches again once the git limits allow"
}

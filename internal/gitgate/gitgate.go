// Package gitgate keeps the git network operations sdd starts within a
// per-machine budget per remote host and protocol: a cooldown per repository
// and kind, retries for transport failures, hourly buckets and a cap on
// concurrent operations per host (20261107-095427-d-tac-yux).
package gitgate

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Kind string

const (
	// Fetch covers fetch, pull, clone and ls-remote.
	Fetch Kind = "fetch"
	// Push covers a push plus the fetch and merge a rejected push needs.
	Push Kind = "push"
)

type Protocol string

const (
	SSH   Protocol = "ssh"
	HTTPS Protocol = "https"
	// Local is file:// and plain paths: never limited, always admitted.
	Local Protocol = "local"
)

// Target names one remote repository.
type Target struct {
	// Host is the lower-cased host name without port; empty for Local.
	Host string
	// Repo is host/path without .git; the path itself for Local.
	Repo     string
	Protocol Protocol
}

type Limits struct {
	FetchCooldown               time.Duration
	PushCooldown                time.Duration
	Retries                     int
	Concurrency                 int
	SSHPerHour                  int
	HTTPSPerHour                int
	HTTPSUnauthenticatedPerHour int
}

// LimitsFunc resolves the limits for one host.
type LimitsFunc func(host string) Limits

type Reason string

const (
	ReasonCooldown  Reason = "cooldown"
	ReasonHourly    Reason = "hourly"
	ReasonBusy      Reason = "busy"
	ReasonRetryWait Reason = "retry_wait"
)

// DeferredError reports an operation the gate did not run.
type DeferredError struct {
	Target Target
	Kind   Kind
	Reason Reason
	// Next is the earliest time the operation may run.
	Next time.Time
}

func (e *DeferredError) Error() string {
	return fmt.Sprintf("%s of %s deferred (%s) until %s", e.Kind, e.Target.Repo, e.Reason, e.Next.Format(time.RFC3339))
}

// ErrNotAdmitted is returned by the git adapter for a network command run on a
// ctx without an admission, so an ungated network call fails loud.
var ErrNotAdmitted = errors.New("git network operation started outside the gate")

type admissionKey struct{}

type admission struct {
	target    Target
	traceFile string
}

// Env returns the environment a git network command must run with and whether
// ctx carries an admission at all. A Local admission adds nothing.
func Env(ctx context.Context) ([]string, bool) {
	adm, ok := ctx.Value(admissionKey{}).(*admission)
	if !ok {
		return nil, false
	}
	if adm.traceFile == "" {
		return []string{}, true
	}
	return []string{"GIT_TRACE2_EVENT=" + adm.traceFile}, true
}

func withAdmission(ctx context.Context, adm *admission) context.Context {
	return context.WithValue(ctx, admissionKey{}, adm)
}

func admissionFrom(ctx context.Context) (*admission, bool) {
	adm, ok := ctx.Value(admissionKey{}).(*admission)
	return adm, ok
}

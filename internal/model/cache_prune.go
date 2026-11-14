package model

import (
	"fmt"
	"strings"
	"time"
)

const (
	// DefaultCacheRetention is how long unused cache data survives background
	// pruning: a replaced index version after its replacement, an unused index
	// store or derivation rule after its last use (20261101-131514-d-cpt-9ci).
	DefaultCacheRetention = "90d"
	// CachePruneInterval is the least time between two background pruning
	// runs on one machine.
	CachePruneInterval = 24 * time.Hour
	// CachePruneRunLimit bounds one pruning run, so a hung run cannot hold
	// the machine's run lock for good; what it did not reach waits for the
	// next run.
	CachePruneRunLimit = 5 * time.Minute
	// CacheLeftoverAge is how old a crash leftover must be before the sweep
	// removes it; a live writer finishes far sooner.
	CacheLeftoverAge = 24 * time.Hour
)

// CacheConfig holds the machine-global cache settings. It lives on the
// user-global config only: the cache is shared by every repository on the
// machine, so a per-repo value would let one repository prune what another
// still uses.
type CacheConfig struct {
	// Retention is days ("90d") or a Go duration. Empty means
	// DefaultCacheRetention.
	Retention string `yaml:"retention,omitempty"`
}

// ResolveCacheRetention returns the effective retention from cfg. A value that
// does not parse is a config error the caller must surface.
func ResolveCacheRetention(cfg CacheConfig) (time.Duration, error) {
	raw := strings.TrimSpace(cfg.Retention)
	if raw == "" {
		raw = DefaultCacheRetention
	}
	d, err := parseDaysOrDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("cache.retention: %q is not a positive duration; use days (%q) or a Go duration (%q)", raw, "90d", "2160h")
	}
	return d, nil
}

// DefaultReplacedRetention is how long replaced index data survives the
// daily sweep after its replacement or last use (20261114-095140-d-cpt-ad4).
const DefaultReplacedRetention = "14d"

// IndexConfig holds the machine-global search index settings; user-global
// only, for the reason CacheConfig is.
type IndexConfig struct {
	// ReplacedRetention is days ("14d") or a Go duration. Empty means
	// DefaultReplacedRetention.
	ReplacedRetention string `yaml:"replaced_retention,omitempty"`
}

// ResolveReplacedRetention returns the effective index.replaced_retention. A
// value that does not parse is a config error the caller must surface.
func ResolveReplacedRetention(cfg IndexConfig) (time.Duration, error) {
	raw := strings.TrimSpace(cfg.ReplacedRetention)
	if raw == "" {
		raw = DefaultReplacedRetention
	}
	d, err := parseDaysOrDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("index.replaced_retention: %q is not a positive duration; use days (%q) or a Go duration (%q)", raw, "14d", "336h")
	}
	return d, nil
}

// CachePruneStatus is the record of the latest background pruning run.
type CachePruneStatus struct {
	// LaunchedAt is when a command last started a background run; StartedAt
	// when a run last began work. Either keeps the next launch a day away.
	LaunchedAt time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	Removed    CachePruneCounts
	// Error is the failure of the latest finished run, empty when it
	// succeeded.
	Error string
	// Reported marks a failure some command has already shown.
	Reported bool
}

// CachePruneCounts tallies what one run deleted.
type CachePruneCounts struct {
	Versions  int
	Stores    int
	Snapshots int
	Clones    int
	// Sessions are ended sessions removed with their staged attachments.
	Sessions int
	// Projects are unused projects whose relevance ledger or session folder
	// went.
	Projects int
	// Records are git gate repository records and embedder calibrations.
	Records int
	// Leftovers are what killed writers left: trace files, clone staging,
	// index temp dirs and orphan rows, retired stores, empty host folders.
	Leftovers int
	// Unreadable counts session logs kept because they could not be read.
	Unreadable int
	Bytes      int64
}

// Add adds what another part of a run removed.
func (c *CachePruneCounts) Add(o CachePruneCounts) {
	c.Versions += o.Versions
	c.Stores += o.Stores
	c.Snapshots += o.Snapshots
	c.Clones += o.Clones
	c.Sessions += o.Sessions
	c.Projects += o.Projects
	c.Records += o.Records
	c.Leftovers += o.Leftovers
	c.Unreadable += o.Unreadable
	c.Bytes += o.Bytes
}

// Due reports whether a background run should start at now.
func (s CachePruneStatus) Due(now time.Time) bool {
	last := s.LaunchedAt
	if s.StartedAt.After(last) {
		last = s.StartedAt
	}
	return now.Sub(last) >= CachePruneInterval
}

// FailureNotice is the one line a command shows for a failed run nobody was
// told about yet, empty otherwise.
func (s CachePruneStatus) FailureNotice() string {
	if s.Error == "" || s.Reported {
		return ""
	}
	return fmt.Sprintf("background cache pruning failed at %s: %s; see sdd info", s.FinishedAt.Format(time.RFC3339), s.Error)
}

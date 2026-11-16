package model

import (
	"strings"
	"time"
)

// The background-sync cooldown and the grep-based graph commit count, kept
// until every caller has moved to the git gate and the subject-only count.

// DefaultSyncCooldown bounds how often background sync runs git fetch
// when last-fetch exceeds this duration. Applied when Config.Sync.Cooldown
// is empty or unparseable.
const DefaultSyncCooldown = "15m"

// ResolveSyncCooldown returns the effective cooldown duration from cfg,
// falling back to DefaultSyncCooldown on empty or unparseable values.
func ResolveSyncCooldown(cfg *PerRepoConfig) time.Duration {
	raw := ""
	if cfg != nil {
		raw = cfg.Sync.Cooldown
	}
	return parsePositiveDuration(raw, DefaultSyncCooldown)
}

// parsePositiveDuration reads a configured duration, falling back to a baked
// default when it is empty, malformed or not positive.
func parsePositiveDuration(raw, fallback string) time.Duration {
	if raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
	}
	d, _ := time.ParseDuration(fallback)
	return d
}

// CountGraphCommits counts non-empty lines from `git log --grep='^sdd:'
// --pretty=format:%H <range>`. Each line is one SDD-written commit in the
// range; we only need the count, not the hashes themselves.
func CountGraphCommits(gitLogOutput string) int {
	n := 0
	for line := range strings.SplitSeq(gitLogOutput, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

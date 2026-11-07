package repos

import (
	"context"
	"strings"
	"sync"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
)

// GitLimitsFunc returns the resolver of the git limits cfg sets per remote
// host, cfg having been read from configPath. A value resolution ignores is
// logged as a warning the first time a host consults it, so a value no
// consulted host uses stays quiet (20260921-174543-d-cpt-tv9).
func GitLimitsFunc(ctx context.Context, cfg *GlobalConfig, configPath string) func(host string) model.GitLimits {
	var syncConfig model.SyncConfig
	if cfg != nil {
		syncConfig = cfg.Sync
	}
	log := slogutils.FromContext(ctx)
	var mu sync.Mutex
	warned := map[string]bool{}
	return func(host string) model.GitLimits {
		limits, warnings := model.ResolveGitLimits(syncConfig, host)
		mu.Lock()
		defer mu.Unlock()
		for _, warning := range warnings {
			if warned[warning] {
				continue
			}
			warned[warning] = true
			key, reason, _ := strings.Cut(warning, ": ")
			log.Warn("ignoring config setting", "key", key, "file", configPath, "reason", reason)
		}
		return limits
	}
}

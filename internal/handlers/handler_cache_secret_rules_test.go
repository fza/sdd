package handlers_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/syncstate"
	"github.com/networkteam/sdd/internal/testsupport"
)

func TestCacheHandlerPruneFetchesSecretRulesPerSetting(t *testing.T) {
	cases := []struct {
		name      string
		config    string
		scheduled bool
		fetches   bool
		fails     bool
	}{
		{name: "daily by default", scheduled: true, fetches: true},
		{name: "daily", config: "secrets:\n  rules_update: daily\n", scheduled: true, fetches: true},
		{name: "an unknown value fails the pass", config: "secrets:\n  rules_update: weekly\n", scheduled: true, fetches: false, fails: true},
		{name: "off", config: "secrets:\n  rules_update: \"off\"\n", scheduled: true, fetches: false},
		{name: "a run by hand", scheduled: false, fetches: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPruneFixture(t, tc.config)
			releases := testsupport.NewGitleaksReleases(t)
			releases.Publish("v99.0.0", testsupport.NewerGitleaksRules(t))
			dir := filepath.Join(f.loc.CacheRoot, secrets.CacheDirName)
			h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
				CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc),
				OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(),
				State:       &recordingStateSweep{},
				SecretRules: secrets.NewUpdater(dir, releases.GitHub(), nil),
			})

			err := h.Prune(t.Context(), &command.PruneCacheCmd{Scheduled: tc.scheduled})

			if tc.fails {
				if err == nil || !strings.Contains(err.Error(), "secrets.rules_update") {
					t.Errorf("the pass should fail naming the setting, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if fetched := len(releases.Requests()) > 0; fetched != tc.fetches {
				t.Errorf("the pass should ask GitHub only while scheduled and daily, asked %v", releases.Requests())
			}
		})
	}
}

// A failed fetch fails the pass after its other work, so the next command
// reports it once, as any failed pass.
func TestCacheHandlerPruneReportsAFailedFetchAndGoesOn(t *testing.T) {
	f := newPruneFixture(t, "")
	releases := testsupport.NewGitleaksReleases(t)
	releases.Publish("v99.0.0", testsupport.SecretRuleFixture(t, "one-rule.toml"))
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc),
		OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(),
		State:       &recordingStateSweep{},
		SecretRules: secrets.NewUpdater(filepath.Join(f.loc.CacheRoot, secrets.CacheDirName), releases.GitHub(), nil),
	})

	err := h.Prune(t.Context(), &command.PruneCacheCmd{Scheduled: true})

	if err == nil || !strings.Contains(err.Error(), "lacks rules") {
		t.Errorf("a rejected rule file should fail the pass with its reason: %v", err)
	}
	status, err := cacheprune.Load(f.loc.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.FailureNotice(), "lacks rules") {
		t.Errorf("the next command should be told of the rejection, status %+v", status)
	}
	if releases.Downloads() == 0 {
		t.Error("the pass should have fetched the rule file it rejects")
	}
	if exists(t, f.staleClone) {
		t.Error("the pass should do its other work")
	}
}

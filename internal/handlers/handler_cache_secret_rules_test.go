package handlers_test

import (
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/syncstate"
	"github.com/networkteam/sdd/test"
)

func TestCacheHandlerPruneFetchesSecretRulesPerSetting(t *testing.T) {
	cases := []struct {
		name      string
		config    string
		scheduled bool
		fetches   bool
	}{
		{name: "daily by default", scheduled: true, fetches: true},
		{name: "daily", config: "secrets:\n  rules_update: daily\n", scheduled: true, fetches: true},
		{name: "an unknown value means the default", config: "secrets:\n  rules_update: weekly\n", scheduled: true, fetches: true},
		{name: "off", config: "secrets:\n  rules_update: \"off\"\n", scheduled: true, fetches: false},
		{name: "a run by hand", scheduled: false, fetches: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPruneFixture(t, tc.config)
			releases := test.NewGitleaksReleases(t)
			releases.Publish("v99.0.0", test.NewerGitleaksRules(t))
			dir := filepath.Join(f.loc.CacheRoot, secrets.CacheDirName)
			h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
				CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc),
				OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(), State: &synctest.StateSweep{},
				SecretRules: secrets.NewUpdater(dir, secrets.NewGitHub(releases.URL, releases.URL, releases.Client), nil),
			})

			err := h.Prune(t.Context(), &command.PruneCacheCmd{Scheduled: tc.scheduled})

			if err != nil {
				t.Fatal(err)
			}
			if fetched := len(releases.Requests()) > 0; fetched != tc.fetches {
				t.Errorf("the pass should ask GitHub only while scheduled and daily, asked %v", releases.Requests())
			}
		})
	}
}

// A failed fetch is the rule file's business: the pass goes on and succeeds.
func TestCacheHandlerPruneGoesOnPastAFailedFetch(t *testing.T) {
	f := newPruneFixture(t, "")
	releases := test.NewGitleaksReleases(t)
	releases.Publish("v99.0.0", test.SecretRuleFixture(t, "one-rule.toml"))
	h := handlers.NewCacheHandler(handlers.CacheHandlerOptions{
		CacheRoot: f.loc.CacheRoot, StateRoot: f.loc.StateRoot, Registry: repos.NewRegistry(f.loc),
		OwnedClones: syncstate.OwnedClones{Gate: f.gate()}, Gate: f.gate(), State: &synctest.StateSweep{},
		SecretRules: secrets.NewUpdater(filepath.Join(f.loc.CacheRoot, secrets.CacheDirName), secrets.NewGitHub(releases.URL, releases.URL, releases.Client), nil),
	})

	err := h.Prune(t.Context(), &command.PruneCacheCmd{Scheduled: true})

	if err != nil {
		t.Fatalf("a rejected rule file should not fail the pass: %v", err)
	}
	if exists(t, f.staleClone) {
		t.Error("the pass should do its other work")
	}
}

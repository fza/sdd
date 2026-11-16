package secrets_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/testsupport"
)

var fetchNow = time.Date(2026, 11, 14, 3, 0, 0, 0, time.UTC)

func newUpdater(releases *testsupport.GitleaksReleases, dir string) *secrets.Updater {
	return secrets.NewUpdater(dir, secrets.NewGitHub(releases.URL, releases.URL, releases.Client), func() time.Time { return fetchNow })
}

func scansFixtureToken(t *testing.T, source *secrets.Source) bool {
	t.Helper()
	set, err := source.Rules()
	if err != nil {
		t.Fatal(err)
	}
	findings, err := set.Scan(testsupport.FixtureToken)
	if err != nil {
		t.Fatal(err)
	}
	return slices.Contains(ruleIDs(findings), "sdd-fixture-token")
}

func status(t *testing.T, dir string) model.SecretRulesStatus {
	t.Helper()
	status, err := secrets.NewSource(t.Context(), dir).Status()
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func TestUpdaterStoresANewerRelease(t *testing.T) {
	releases := testsupport.NewGitleaksReleases(t)
	releases.Publish("v99.0.0", testsupport.NewerGitleaksRules(t))
	dir := t.TempDir()

	err := newUpdater(releases, dir).Update(t.Context())

	if err != nil {
		t.Fatal(err)
	}
	got := status(t, dir)
	if got.Source != model.SecretRulesFetched || got.Tag != "v99.0.0" || !got.LastAccepted.Equal(fetchNow) {
		t.Errorf("the newer release should be stored and in use, got %+v", got)
	}
	if !scansFixtureToken(t, secrets.NewSource(t.Context(), dir)) {
		t.Error("a scan should apply the rule the newer release added")
	}
}

func TestUpdaterSkipsTheDownloadWithoutANewerRelease(t *testing.T) {
	builtin, err := secrets.BuiltinRuleFile()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		before func(t *testing.T, releases *testsupport.GitleaksReleases, dir string)
		tag    string
	}{
		{name: "the built-in release", tag: builtin.Tag},
		{name: "the fetched release", tag: "v99.0.0", before: func(t *testing.T, releases *testsupport.GitleaksReleases, dir string) {
			releases.Publish("v99.0.0", testsupport.NewerGitleaksRules(t))
			err := newUpdater(releases, dir).Update(t.Context())
			if err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			releases := testsupport.NewGitleaksReleases(t)
			dir := t.TempDir()
			if tc.before != nil {
				tc.before(t, releases, dir)
			}
			releases.Publish(tc.tag, testsupport.NewerGitleaksRules(t))
			downloads := releases.Downloads()

			err := newUpdater(releases, dir).Update(t.Context())

			if err != nil {
				t.Fatal(err)
			}
			if releases.Downloads() != downloads {
				t.Error("a release no newer than the copy in use should not be downloaded")
			}
			if got := status(t, dir); got.Tag != tc.tag || !got.LastChecked.Equal(fetchNow) {
				t.Errorf("the check should be recorded and the copy kept, got %+v", got)
			}
		})
	}
}

func TestUpdaterKeepsTheCopyInUseOnAFailure(t *testing.T) {
	cases := []struct {
		name   string
		fail   func(t *testing.T, releases *testsupport.GitleaksReleases)
		reason string
	}{
		{name: "copy lacking built-in rules", reason: "lacks rules", fail: func(t *testing.T, releases *testsupport.GitleaksReleases) {
			releases.Publish("v100.0.0", testsupport.SecretRuleFixture(t, "one-rule.toml"))
		}},
		{name: "copy that stops flagging the built-in samples", reason: "no longer flags", fail: func(t *testing.T, releases *testsupport.GitleaksReleases) {
			releases.Publish("v100.0.0", append(testsupport.NewerGitleaksRules(t), "\n[[allowlists]]\nregexes = ['''.''']\n"...))
		}},
		{name: "copy with an unknown key", reason: "unknown keys", fail: func(t *testing.T, releases *testsupport.GitleaksReleases) {
			releases.Publish("v100.0.0", append(testsupport.NewerGitleaksRules(t), "\nsddVersion = 1\n"...))
		}},
		{name: "copy with a pattern that does not compile", reason: "sdd-fixture-file", fail: func(t *testing.T, releases *testsupport.GitleaksReleases) {
			releases.Publish("v100.0.0", append(testsupport.NewerGitleaksRules(t), testsupport.SecretRuleFixture(t, "bad-rule-path.toml")...))
		}},
		{name: "pre-release tag", reason: "no release version", fail: func(t *testing.T, releases *testsupport.GitleaksReleases) {
			releases.Publish("v100.0.0-rc.1", testsupport.NewerGitleaksRules(t))
		}},
		{name: "unreachable release API", reason: "500", fail: func(t *testing.T, releases *testsupport.GitleaksReleases) {
			releases.FailWith(http.StatusInternalServerError)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			releases := testsupport.NewGitleaksReleases(t)
			releases.Publish("v99.0.0", testsupport.NewerGitleaksRules(t))
			dir := t.TempDir()
			updater := newUpdater(releases, dir)
			err := updater.Update(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			tc.fail(t, releases)

			err = updater.Update(t.Context())

			if err == nil {
				t.Fatal("the failed fetch should be reported")
			}
			got := status(t, dir)
			if got.Source != model.SecretRulesFetched || got.Tag != "v99.0.0" {
				t.Errorf("the copy in use should stay, got %+v", got)
			}
			if !strings.Contains(got.LastFailure, tc.reason) || !got.LastFailureAt.Equal(fetchNow) {
				t.Errorf("the failure should be recorded with its reason, got %+v", got)
			}
			if !scansFixtureToken(t, secrets.NewSource(t.Context(), dir)) {
				t.Error("scans should go on with the copy in use")
			}
		})
	}
}

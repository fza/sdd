package secrets_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/test"
)

// A long-running server rechecks through Use on its timer; the next scan
// applies a copy fetched since, without a restart.
func TestUsePicksUpACopyFetchedAfterTheFirstScan(t *testing.T) {
	dir := t.TempDir()
	secrets.Use(t.Context(), dir)
	t.Cleanup(func() { secrets.Use(t.Context(), "") })
	scan := func() bool {
		findings, err := secrets.Scan(test.FixtureToken)
		if err != nil {
			t.Fatal(err)
		}
		return slices.Contains(ruleIDs(findings), "sdd-fixture-token")
	}
	if scan() {
		t.Fatal("the built-in rules should not know the fixture rule")
	}
	releases := test.NewGitleaksReleases(t)
	releases.Publish("v99.0.0", test.NewerGitleaksRules(t))
	err := newUpdater(releases, dir).Update(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	secrets.Use(t.Context(), dir)

	if !scan() {
		t.Error("the next scan should apply the newly fetched copy")
	}
}

func TestSourceWithoutAFolderUsesTheBuiltinFile(t *testing.T) {
	dir := t.TempDir()
	releases := test.NewGitleaksReleases(t)
	releases.Publish("v99.0.0", test.NewerGitleaksRules(t))
	err := newUpdater(releases, dir).Update(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	source := secrets.NewSource(t.Context(), "")

	got, err := source.Status()

	if err != nil {
		t.Fatal(err)
	}
	if got.Source != model.SecretRulesBuiltIn || got.Update != model.SecretRulesOff || scansFixtureToken(t, source) {
		t.Errorf("updates off should scan with the built-in file alone, got %+v", got)
	}
}

func TestSourceSetsAsideAChangedCopy(t *testing.T) {
	dir := t.TempDir()
	releases := test.NewGitleaksReleases(t)
	releases.Publish("v99.0.0", test.NewerGitleaksRules(t))
	err := newUpdater(releases, dir).Update(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	copies, err := filepath.Glob(filepath.Join(dir, "gitleaks-*.toml"))
	if err != nil || len(copies) != 1 {
		t.Fatalf("one stored copy expected, got %v, %v", copies, err)
	}
	err = os.WriteFile(copies[0], test.SecretRuleFixture(t, "bad-rule-regex.toml"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	source := secrets.NewSource(t.Context(), dir)

	got, err := source.Status()

	if err != nil {
		t.Fatal(err)
	}
	if got.Source != model.SecretRulesBuiltIn || !strings.Contains(got.Unused, "changed") {
		t.Errorf("a copy changed after its acceptance should give way to the built-in file, got %+v", got)
	}
}

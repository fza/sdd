package secrets_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/testsupport"
)

// floor.json holds, per built-in rule, the first of gitleaks' own true
// positives, or of the synthetic ones for a rule gitleaks gives none; a
// fetched copy must still flag each. UPDATE_SNAPSHOTS=true
// writes it from the fixtures after the built-in file changes.
func TestFloorSamplesFollowTheGitleaksFixtures(t *testing.T) {
	builtin, err := secrets.BuiltinRuleFile()
	if err != nil {
		t.Fatal(err)
	}
	set, err := secrets.ParseRules(builtin.Data)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := testsupport.SecretFixtures()
	var paths []string
	for _, dir := range []string{"gitleaks", "synthetic"} {
		files, err := fs.ReadDir(fixtures, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			paths = append(paths, path.Join(dir, file.Name()))
		}
	}
	var floor []secrets.FloorSample
	for _, file := range paths {
		data, err := fs.ReadFile(fixtures, file)
		if err != nil {
			t.Fatal(err)
		}
		var samples gitleaksSamples
		err = json.Unmarshal(data, &samples)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if !slices.Contains(set.RuleIDs(), samples.Rule) || len(samples.TruePositives) == 0 {
			continue
		}
		first := samples.TruePositives[0]
		floor = append(floor, secrets.FloorSample{Rule: samples.Rule, Path: first.Path, Text: first.Text})
	}
	slices.SortFunc(floor, func(a, b secrets.FloorSample) int { return strings.Compare(a.Rule, b.Rule) })
	want, err := json.MarshalIndent(floor, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')

	if os.Getenv("UPDATE_SNAPSHOTS") == "true" {
		err = os.WriteFile("floor.json", want, 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile("floor.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("floor.json should hold the first true positive of every built-in rule gitleaks' fixtures cover; rerun with UPDATE_SNAPSHOTS=true")
	}
}

// A copy that keeps every rule ID yet stops flagging what the built-in rules
// flag weakens the scan: it is refused.
func TestParseFetchedRulesRefusesACopyThatStopsFlaggingTheSamples(t *testing.T) {
	cases := []struct {
		name    string
		appendx string
		missing string
	}{
		{name: "global allowlist matching everything", appendx: "\n[[allowlists]]\nregexes = ['''.''']\n", missing: "github-pat"},
		{name: "global allowlist matching every line", appendx: "\n[[allowlists]]\nregexTarget = \"line\"\nregexes = ['''(?s).*''']\n", missing: "github-pat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builtin, err := secrets.BuiltinRuleFile()
			if err != nil {
				t.Fatal(err)
			}

			_, err = secrets.ParseFetchedRules(append(append([]byte(nil), builtin.Data...), tc.appendx...))

			if err == nil || !strings.Contains(err.Error(), tc.missing) {
				t.Errorf("the copy should be refused naming the rules it no longer flags, got %v", err)
			}
		})
	}
}

// A built-in rule without a sample would be checked by its ID alone, which a
// loosened pattern keeps.
func TestFloorCoversEveryBuiltinRule(t *testing.T) {
	builtin, err := secrets.BuiltinRuleFile()
	if err != nil {
		t.Fatal(err)
	}
	set, err := secrets.ParseRules(builtin.Data)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("floor.json")
	if err != nil {
		t.Fatal(err)
	}
	var floor []secrets.FloorSample
	err = json.Unmarshal(data, &floor)
	if err != nil {
		t.Fatal(err)
	}

	var uncovered []string
	for _, id := range set.RuleIDs() {
		if !slices.ContainsFunc(floor, func(s secrets.FloorSample) bool { return s.Rule == id }) {
			uncovered = append(uncovered, id)
		}
	}

	if len(uncovered) > 0 {
		t.Errorf("every built-in rule should have a sample; add one under fixtures/secrets/synthetic for: %v", uncovered)
	}
}

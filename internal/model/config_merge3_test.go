package model_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestMergeConfigYAML(t *testing.T) {
	const base = "# upstream comment\ngraph_dir: .sdd/graph\nlanguage: en\ndependencies:\n  - github.com/acme/a\n  - github.com/acme/b\nllm:\n  model: m1\n"
	cases := []struct {
		name        string
		ours        string
		theirs      string
		check       func(t *testing.T, merged *model.PerRepoConfig, raw string)
		wantNotices []string
	}{
		{
			name:   "a key changed only on our side is taken",
			ours:   strings.Replace(base, "language: en", "language: de", 1),
			theirs: strings.Replace(base, "m1", "m2", 1),
			check: func(t *testing.T, merged *model.PerRepoConfig, _ string) {
				if merged.Language != "de" || merged.LLM.Model != "m2" {
					t.Errorf("each side's own change should land, got language %q model %q", merged.Language, merged.LLM.Model)
				}
			},
		},
		{
			name:   "dependencies merge as a set of both sides' additions and removals",
			ours:   strings.Replace(base, "  - github.com/acme/a\n", "  - github.com/acme/a\n  - github.com/acme/c\n", 1),
			theirs: strings.Replace(base, "  - github.com/acme/b\n", "  - github.com/acme/d\n", 1),
			check: func(t *testing.T, merged *model.PerRepoConfig, _ string) {
				got := make([]string, 0, len(merged.Dependencies))
				for _, dep := range merged.Dependencies {
					got = append(got, string(dep))
				}
				slices.Sort(got)
				if !slices.Equal(got, []string{"github.com/acme/a", "github.com/acme/c", "github.com/acme/d"}) {
					t.Errorf("additions from both sides should stay and removals from both apply, got %v", got)
				}
			},
		},
		{
			name:        "a scalar changed on both sides takes upstream with a notice",
			ours:        strings.Replace(base, "m1", "ours", 1),
			theirs:      strings.Replace(base, "m1", "theirs", 1),
			wantNotices: []string{"llm.model"},
			check: func(t *testing.T, merged *model.PerRepoConfig, _ string) {
				if merged.LLM.Model != "theirs" {
					t.Errorf("the upstream value should win")
				}
			},
		},
		{
			name:   "a key removed on our side is removed",
			ours:   strings.Replace(base, "language: en\n", "", 1),
			theirs: base,
			check: func(t *testing.T, merged *model.PerRepoConfig, _ string) {
				if merged.Language != "" {
					t.Errorf("our removal should land")
				}
			},
		},
		{
			name:   "comments follow upstream",
			ours:   strings.Replace(base, "language: en", "language: de", 1),
			theirs: strings.Replace(base, "# upstream comment", "# changed upstream", 1),
			check: func(t *testing.T, _ *model.PerRepoConfig, raw string) {
				if !strings.Contains(raw, "# changed upstream") {
					t.Errorf("upstream comments should survive:\n%s", raw)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			merged, notices, err := model.MergeConfigYAML([]byte(base), []byte(tc.ours), []byte(tc.theirs))
			if err != nil {
				t.Fatal(err)
			}
			if len(notices) != len(tc.wantNotices) {
				t.Fatalf("notices should name exactly the keys changed on both sides, got %v", notices)
			}
			for i, key := range tc.wantNotices {
				if !strings.Contains(notices[i], key) {
					t.Errorf("the notice should name the key, got %q", notices[i])
				}
			}
			cfg, err := model.ParseConfig(merged)
			if err != nil {
				t.Fatalf("the result should parse: %v\n%s", err, merged)
			}
			tc.check(t, cfg, string(merged))
		})
	}
}

func TestMergeConfigYAMLIsSymmetricForOneSidedChanges(t *testing.T) {
	const base = "language: en\n"
	merged, _, err := model.MergeConfigYAML([]byte(base), []byte(base), []byte("language: fr\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := model.ParseConfig(merged)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "fr" {
		t.Errorf("an upstream-only change should land")
	}
}

package secrets

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

//go:embed gitleaks.toml
var builtinTOML []byte

// The built-in file is trusted (its tests compile every pattern), so it loads
// without compiling: a rule's patterns compile on first need.
var builtinRules = sync.OnceValues(func() (*RuleSet, error) {
	set, err := decodeRules(builtinTOML)
	if err != nil {
		return nil, fmt.Errorf("built-in rule file: %w", err)
	}
	return set, nil
})

// gitleaksFile mirrors the keys of gitleaks' default rule file. Decoding
// refuses any other key, so a release that adds one is rejected until sdd
// knows what it means.
type gitleaksFile struct {
	Title       string              `toml:"title"`
	Description string              `toml:"description"`
	MinVersion  string              `toml:"minVersion"`
	Allowlist   *gitleaksAllowlist  `toml:"allowlist"`
	Allowlists  []gitleaksAllowlist `toml:"allowlists"`
	Rules       []gitleaksRule      `toml:"rules"`
}

type gitleaksRule struct {
	ID          string              `toml:"id"`
	Description string              `toml:"description"`
	Regex       string              `toml:"regex"`
	Path        string              `toml:"path"`
	Entropy     float64             `toml:"entropy"`
	SecretGroup int                 `toml:"secretGroup"`
	Keywords    []string            `toml:"keywords"`
	Allowlist   *gitleaksAllowlist  `toml:"allowlist"`
	Allowlists  []gitleaksAllowlist `toml:"allowlists"`
}

type gitleaksAllowlist struct {
	Description string   `toml:"description"`
	Condition   string   `toml:"condition"`
	RegexTarget string   `toml:"regexTarget"`
	Regexes     []string `toml:"regexes"`
	Paths       []string `toml:"paths"`
	Stopwords   []string `toml:"stopwords"`
}

// ParseRules reads a gitleaks rule file strictly and compiles every pattern
// in it; any unknown key or pattern that does not compile refuses the file.
func ParseRules(data []byte) (*RuleSet, error) {
	set, err := decodeRules(data)
	if err != nil {
		return nil, err
	}
	for _, r := range set.rules {
		err = r.compilePatterns()
		if err != nil {
			return nil, err
		}
	}
	return set, nil
}

func decodeRules(data []byte) (*RuleSet, error) {
	var file gitleaksFile
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&file)
	if err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return nil, fmt.Errorf("decoding the rule file: unknown keys:\n%s", strict.String())
		}
		return nil, fmt.Errorf("decoding the rule file: %w", err)
	}
	set := &RuleSet{allowlists: allowlistsOf(file.Allowlist, file.Allowlists)}
	for i := range set.allowlists {
		err = set.allowlists[i].compilePatterns()
		if err != nil {
			return nil, fmt.Errorf("global allowlist: %w", err)
		}
	}
	for _, entry := range file.Rules {
		keywords := make([]string, 0, len(entry.Keywords))
		for _, keyword := range entry.Keywords {
			keywords = append(keywords, strings.ToLower(keyword))
		}
		slices.Sort(keywords)
		set.rules = append(set.rules, &rule{
			ID:          entry.ID,
			Regex:       entry.Regex,
			Path:        entry.Path,
			Entropy:     entry.Entropy,
			SecretGroup: entry.SecretGroup,
			Keywords:    slices.Compact(keywords),
			Allowlists:  allowlistsOf(entry.Allowlist, entry.Allowlists),
		})
	}
	return set, nil
}

// allowlistsOf reads gitleaks' single [allowlist] table and its
// [[allowlists]] array alike, as gitleaks does: stopwords lowercased, the
// condition uppercased.
func allowlistsOf(single *gitleaksAllowlist, multiple []gitleaksAllowlist) []allowlist {
	var entries []gitleaksAllowlist
	if single != nil {
		entries = append(entries, *single)
	}
	entries = append(entries, multiple...)
	lists := make([]allowlist, 0, len(entries))
	for _, entry := range entries {
		stopwords := make([]string, 0, len(entry.Stopwords))
		for _, word := range entry.Stopwords {
			stopwords = append(stopwords, strings.ToLower(word))
		}
		lists = append(lists, allowlist{
			Condition:   strings.ToUpper(entry.Condition),
			RegexTarget: entry.RegexTarget,
			Regexes:     entry.Regexes,
			Paths:       entry.Paths,
			Stopwords:   stopwords,
		})
	}
	return lists
}

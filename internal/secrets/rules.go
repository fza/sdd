package secrets

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
	"golang.org/x/mod/semver"
)

//go:embed gitleaks.toml
var builtinTOML []byte

//go:embed gitleaks.source.json
var builtinSourceJSON []byte

// RuleFile is one copy of gitleaks' config/gitleaks.toml and the release tag
// it was published under.
type RuleFile struct {
	Tag  string
	Data []byte
}

// BuiltinStamp records where the built-in rule file came from; the refresh
// program writes it beside gitleaks.toml.
type BuiltinStamp struct {
	Repository string `json:"repository"`
	File       string `json:"file"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
}

var builtinStamp = sync.OnceValues(func() (BuiltinStamp, error) {
	var stamp BuiltinStamp
	err := json.Unmarshal(builtinSourceJSON, &stamp)
	if err != nil {
		return BuiltinStamp{}, fmt.Errorf("decoding the built-in rule file's stamp: %w", err)
	}
	return stamp, nil
})

// BuiltinRuleFile is the rule file compiled into the binary.
func BuiltinRuleFile() (RuleFile, error) {
	stamp, err := builtinStamp()
	if err != nil {
		return RuleFile{}, err
	}
	return RuleFile{Tag: stamp.Tag, Data: builtinTOML}, nil
}

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

// ParseFetchedRules is ParseRules for a copy fetched to replace the built-in
// file, which must also keep every rule the built-in file has.
func ParseFetchedRules(data []byte) (*RuleSet, error) {
	set, err := ParseRules(data)
	if err != nil {
		return nil, err
	}
	err = coversBuiltin(set)
	if err != nil {
		return nil, err
	}
	err = keepsTheFloor(set)
	if err != nil {
		return nil, err
	}
	err = boundsTheScanCost(set)
	if err != nil {
		return nil, err
	}
	return set, nil
}

func coversBuiltin(set *RuleSet) error {
	builtin, err := builtinRules()
	if err != nil {
		return err
	}
	missing := set.missing(builtin)
	if len(missing) > 0 {
		return fmt.Errorf("the rule file lacks rules the built-in file has: %s", strings.Join(missing, ", "))
	}
	return nil
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
	err = readableVersion(file.MinVersion)
	if err != nil {
		return nil, err
	}
	set := &RuleSet{allowlists: allowlistsOf(file.Allowlist, file.Allowlists)}
	err = readableAllowlists(set.allowlists)
	if err != nil {
		return nil, fmt.Errorf("global allowlist: %w", err)
	}
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
		allowlists := allowlistsOf(entry.Allowlist, entry.Allowlists)
		err = readableAllowlists(allowlists)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", entry.ID, err)
		}
		set.rules = append(set.rules, &rule{
			ID:          entry.ID,
			Regex:       entry.Regex,
			Path:        entry.Path,
			Entropy:     entry.Entropy,
			SecretGroup: entry.SecretGroup,
			Keywords:    slices.Compact(keywords),
			Allowlists:  allowlists,
		})
	}
	return set, nil
}

// readsGitleaksVersion is the gitleaks release whose rule file semantics the
// scanner implements; a file asking for a newer gitleaks is refused.
const readsGitleaksVersion = "v8.30.1"

func readableVersion(minVersion string) error {
	if minVersion == "" {
		return nil
	}
	if !semver.IsValid(minVersion) {
		return fmt.Errorf("the rule file's minVersion %q is no version", minVersion)
	}
	if semver.Compare(minVersion, readsGitleaksVersion) > 0 {
		return fmt.Errorf("the rule file needs gitleaks %s; sdd reads rule files up to gitleaks %s", minVersion, readsGitleaksVersion)
	}
	return nil
}

// readableAllowlists refuses a condition or regexTarget the scan does not
// implement, which it would otherwise read as the default.
func readableAllowlists(lists []allowlist) error {
	for _, a := range lists {
		if !slices.Contains([]string{"", "OR", "AND"}, a.Condition) {
			return fmt.Errorf("allowlist condition %q is not OR or AND", a.Condition)
		}
		if !slices.Contains([]string{"", "secret", "match", "line"}, a.RegexTarget) {
			return fmt.Errorf("allowlist regexTarget %q is not secret, match or line", a.RegexTarget)
		}
	}
	return nil
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

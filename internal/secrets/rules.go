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
// file. The copy must keep every rule the built-in file has, cost a scan at
// most maxScanCostFactor times what the built-in file costs, and raise each
// built-in rule on its sample in gitleaks.samples.json.
func ParseFetchedRules(data []byte) (*RuleSet, error) {
	set, err := ParseRules(data)
	if err != nil {
		return nil, err
	}
	err = coversBuiltin(set)
	if err != nil {
		return nil, err
	}
	err = withinScanCost(set)
	if err != nil {
		return nil, err
	}
	err = raisesBuiltinSamples(set)
	if err != nil {
		return nil, err
	}
	return set, nil
}

// maxScanCostFactor leaves room for releases adding rules, none for a file
// that slows every scan by orders of magnitude.
const maxScanCostFactor = 2

func withinScanCost(set *RuleSet) error {
	builtin, err := builtinRules()
	if err != nil {
		return err
	}
	limit, err := builtin.scanCost()
	if err != nil {
		return err
	}
	cost, err := set.scanCost()
	if err != nil {
		return err
	}
	if cost > maxScanCostFactor*limit {
		return fmt.Errorf("a scan with the rule file would cost %d, more than %d times the built-in file's %d", cost, maxScanCostFactor, limit)
	}
	return nil
}

// gitleaks.samples.json holds the shortest true positive per rule of
// test/fixtures/secrets/gitleaks, plus synthetic ones for the two dropbox
// rules gitleaks lists none for. A built-in file with a new rule needs its
// sample here, or every fetched copy is refused.
//
//go:embed gitleaks.samples.json
var builtinSamplesJSON []byte

// sample is a text its rule must raise; one with a path is a file.
type sample struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

var builtinSamples = sync.OnceValues(func() (map[string]sample, error) {
	var samples map[string]sample
	err := json.Unmarshal(builtinSamplesJSON, &samples)
	if err != nil {
		return nil, fmt.Errorf("decoding the built-in rules' samples: %w", err)
	}
	return samples, nil
})

// raisesBuiltinSamples refuses a set that keeps a built-in rule's id but no
// longer finds what it found, as a loosened pattern or a global allowlist
// would.
func raisesBuiltinSamples(set *RuleSet) error {
	builtin, err := builtinRules()
	if err != nil {
		return err
	}
	samples, err := builtinSamples()
	if err != nil {
		return err
	}
	var silent []string
	for _, id := range builtin.RuleIDs() {
		s, ok := samples[id]
		if !ok {
			return fmt.Errorf("the built-in rule %s has no sample in gitleaks.samples.json", id)
		}
		findings, err := set.scan(s.Path, s.Text)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(findings, func(f Finding) bool { return f.RuleID == id }) {
			silent = append(silent, id)
		}
	}
	if len(silent) == 0 {
		return nil
	}
	named := silent[:min(len(silent), 5)]
	more := ""
	if len(silent) > len(named) {
		more = fmt.Sprintf(" and %d more", len(silent)-len(named))
	}
	return fmt.Errorf("with the rule file, built-in rules no longer raise their samples: %s%s", strings.Join(named, ", "), more)
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
	if file.MinVersion != "" && (!semver.IsValid(file.MinVersion) || semver.Compare(file.MinVersion, readsGitleaksVersion) > 0) {
		return nil, fmt.Errorf("the rule file needs gitleaks %s, sdd reads rule files as gitleaks %s does", file.MinVersion, readsGitleaksVersion)
	}
	global, err := allowlistsOf(file.Allowlist, file.Allowlists)
	if err != nil {
		return nil, fmt.Errorf("global allowlist: %w", err)
	}
	set := &RuleSet{allowlists: global}
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
		lists, err := allowlistsOf(entry.Allowlist, entry.Allowlists)
		if err != nil {
			return nil, fmt.Errorf("rule %s allowlist: %w", entry.ID, err)
		}
		set.rules = append(set.rules, &rule{
			ID:          entry.ID,
			Regex:       entry.Regex,
			Path:        entry.Path,
			Entropy:     entry.Entropy,
			SecretGroup: entry.SecretGroup,
			Keywords:    slices.Compact(keywords),
			Allowlists:  lists,
		})
	}
	return set, nil
}

// readsGitleaksVersion is the gitleaks release whose reading of a rule file
// the scan follows; a file needing a later release is refused.
const readsGitleaksVersion = "v8.30.1"

// allowlistsOf reads gitleaks' single [allowlist] table and its
// [[allowlists]] array alike, as gitleaks does: stopwords lowercased, the
// condition uppercased. A condition or regexTarget gitleaks does not know is
// refused, not read as the default.
func allowlistsOf(single *gitleaksAllowlist, multiple []gitleaksAllowlist) ([]allowlist, error) {
	var entries []gitleaksAllowlist
	if single != nil {
		entries = append(entries, *single)
	}
	entries = append(entries, multiple...)
	lists := make([]allowlist, 0, len(entries))
	for _, entry := range entries {
		condition := strings.ToUpper(entry.Condition)
		if !slices.Contains([]string{"", "OR", "AND"}, condition) {
			return nil, fmt.Errorf("condition %q is neither AND nor OR", entry.Condition)
		}
		if !slices.Contains([]string{"", "secret", "match", "line"}, entry.RegexTarget) {
			return nil, fmt.Errorf("regexTarget %q is none of secret, match, line", entry.RegexTarget)
		}
		stopwords := make([]string, 0, len(entry.Stopwords))
		for _, word := range entry.Stopwords {
			stopwords = append(stopwords, strings.ToLower(word))
		}
		lists = append(lists, allowlist{
			Condition:   condition,
			RegexTarget: entry.RegexTarget,
			Regexes:     entry.Regexes,
			Paths:       entry.Paths,
			Stopwords:   stopwords,
		})
	}
	return lists, nil
}

// Package secrets finds credentials in text bound for the graph, using
// gitleaks' default rule file as gitleaks publishes it: the copy built into
// the binary, or a newer release the daily pass fetched. A finding refuses the write: an entry is shared and immutable, and a
// redacted copy beside a missed variant of the same credential is worse than
// a refusal. Findings name the rule and the line, never the matched text.
package secrets

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
)

type Finding struct {
	RuleID string
	Line   int
}

type allowlist struct {
	Condition   string
	RegexTarget string
	Regexes     []string
	Paths       []string
	Stopwords   []string

	regexes []*regexp.Regexp
	paths   []*regexp.Regexp
}

type rule struct {
	ID          string
	Regex       string
	Path        string
	Entropy     float64
	SecretGroup int
	Keywords    []string
	Allowlists  []allowlist

	// Patterns compile on first need, so text holding none of a rule's
	// keywords never pays for its regex.
	compile sync.Once
	regex   *regexp.Regexp
	path    *regexp.Regexp
	err     error
}

// RuleSet is one gitleaks rule file, read and normalized.
type RuleSet struct {
	allowlists []allowlist
	rules      []*rule
}

// RuleIDs lists the set's rules by id, in file order.
func (s *RuleSet) RuleIDs() []string {
	ids := make([]string, 0, len(s.rules))
	for _, r := range s.rules {
		ids = append(ids, r.ID)
	}
	return ids
}

// missing lists the rule ids of floor the set lacks.
func (s *RuleSet) missing(floor *RuleSet) []string {
	have := s.RuleIDs()
	var missing []string
	for _, id := range floor.RuleIDs() {
		if !slices.Contains(have, id) {
			missing = append(missing, id)
		}
	}
	return missing
}

// Scan reports the credentials in text that has no file path, such as an
// entry's title, body or summary, using the active rule set (see Use). Rules
// scoped to file paths never apply.
func Scan(text string) ([]Finding, error) {
	set, err := Active().Rules()
	if err != nil {
		return nil, err
	}
	return set.Scan(text)
}

// ScanFile reports the credentials in a file's content; rules and allowlists
// scoped to file paths apply to path.
func ScanFile(path string, content []byte) ([]Finding, error) {
	set, err := Active().Rules()
	if err != nil {
		return nil, err
	}
	return set.ScanFile(path, content)
}

// Scan reports the credentials the set finds in text without a file path.
func (s *RuleSet) Scan(text string) ([]Finding, error) {
	return s.scan("", text)
}

// ScanFile reports the credentials the set finds in a file's content.
func (s *RuleSet) ScanFile(path string, content []byte) ([]Finding, error) {
	return s.scan(path, string(content))
}

func (s *RuleSet) scan(path, text string) ([]Finding, error) {
	if path != "" && pathAllowed(s.allowlists, path) {
		return nil, nil
	}
	lowered := strings.ToLower(text)
	var findings []Finding
	for _, r := range s.rules {
		if !r.mayMatch(lowered) {
			continue
		}
		err := r.compilePatterns()
		if err != nil {
			return nil, err
		}
		found := r.find(path, text, s.allowlists)
		findings = append(findings, found...)
	}
	slices.SortStableFunc(findings, func(a, b Finding) int { return a.Line - b.Line })
	return findings, nil
}

func (r *rule) mayMatch(lowered string) bool {
	if len(r.Keywords) == 0 {
		return true
	}
	for _, keyword := range r.Keywords {
		if strings.Contains(lowered, keyword) {
			return true
		}
	}
	return false
}

func (r *rule) compilePatterns() error {
	r.compile.Do(func() {
		if r.Regex != "" {
			r.regex, r.err = regexp.Compile(r.Regex)
			if r.err != nil {
				r.err = fmt.Errorf("rule %s: %w", r.ID, r.err)
				return
			}
		}
		if r.Path != "" {
			r.path, r.err = regexp.Compile(r.Path)
			if r.err != nil {
				r.err = fmt.Errorf("rule %s path: %w", r.ID, r.err)
				return
			}
		}
		for i := range r.Allowlists {
			r.err = r.Allowlists[i].compilePatterns()
			if r.err != nil {
				r.err = fmt.Errorf("rule %s allowlist: %w", r.ID, r.err)
				return
			}
		}
	})
	return r.err
}

func (r *rule) find(path, text string, global []allowlist) []Finding {
	if r.path != nil {
		if path == "" || !r.path.MatchString(path) {
			return nil
		}
		if r.regex == nil {
			return []Finding{{RuleID: r.ID, Line: 1}}
		}
	}
	if r.regex == nil {
		return nil
	}
	if path != "" && pathAllowed(r.Allowlists, path) {
		return nil
	}
	var findings []Finding
	for _, bounds := range r.regex.FindAllStringIndex(text, -1) {
		match := strings.Trim(text[bounds[0]:bounds[1]], "\n")
		secret := r.secret(match)
		if secret == "" {
			continue
		}
		if r.Entropy != 0 && shannonEntropy(secret) <= r.Entropy {
			continue
		}
		if r.ID == genericAPIKeyRule && !credentialShaped(secret) {
			continue
		}
		line := linesAround(text, bounds[0], bounds[0]+len(match))
		if allowed(global, path, secret, match, line) || allowed(r.Allowlists, path, secret, match, line) {
			continue
		}
		findings = append(findings, Finding{RuleID: r.ID, Line: strings.Count(text[:bounds[0]], "\n") + 1})
	}
	return findings
}

// secret is the part of a match the entropy floor and allowlists judge: the
// rule's secretGroup, else its first non-empty group, else the whole match.
func (r *rule) secret(match string) string {
	groups := r.regex.FindStringSubmatch(match)
	if len(groups) < 2 {
		return match
	}
	if r.SecretGroup > 0 {
		if len(groups) <= r.SecretGroup {
			return ""
		}
		return groups[r.SecretGroup]
	}
	for _, group := range groups[1:] {
		if group != "" {
			return group
		}
	}
	return match
}

func (a *allowlist) compilePatterns() error {
	if a.regexes != nil || a.paths != nil {
		return nil
	}
	var err error
	a.regexes, err = compileAll(a.Regexes)
	if err != nil {
		return err
	}
	a.paths, err = compileAll(a.Paths)
	return err
}

func compileAll(patterns []string) ([]*regexp.Regexp, error) {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, re)
	}
	return compiled, nil
}

// pathAllowed reports whether an allowlist skips the whole file: an OR
// allowlist naming the path, or an AND allowlist checking nothing but paths.
func pathAllowed(lists []allowlist, path string) bool {
	for i := range lists {
		a := &lists[i]
		if len(a.paths) == 0 || !anyMatch(a.paths, path) {
			continue
		}
		if a.Condition != "AND" || (len(a.regexes) == 0 && len(a.Stopwords) == 0) {
			return true
		}
	}
	return false
}

func allowed(lists []allowlist, path, secret, match, line string) bool {
	for i := range lists {
		a := &lists[i]
		target := secret
		switch a.RegexTarget {
		case "match":
			target = match
		case "line":
			target = line
		}
		regexAllowed := target != "" && anyMatch(a.regexes, target)
		stopword := containsStopword(a.Stopwords, secret)
		if a.Condition != "AND" {
			if regexAllowed || stopword {
				return true
			}
			continue
		}
		checks := 0
		passed := 0
		if len(a.paths) > 0 {
			checks++
			if path != "" && anyMatch(a.paths, path) {
				passed++
			}
		}
		if len(a.regexes) > 0 {
			checks++
			if regexAllowed {
				passed++
			}
		}
		if len(a.Stopwords) > 0 {
			checks++
			if stopword {
				passed++
			}
		}
		if checks > 0 && passed == checks {
			return true
		}
	}
	return false
}

func anyMatch(patterns []*regexp.Regexp, s string) bool {
	for _, re := range patterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func containsStopword(stopwords []string, secret string) bool {
	if secret == "" {
		return false
	}
	lowered := strings.ToLower(secret)
	for _, word := range stopwords {
		if strings.Contains(lowered, word) {
			return true
		}
	}
	return false
}

// linesAround is the whole of every line the match spans, which the
// allowlists' "line" target reads.
func linesAround(text string, from, to int) string {
	start := strings.LastIndexByte(text[:from], '\n') + 1
	end := strings.IndexByte(text[to:], '\n')
	if end < 0 {
		return text[start:]
	}
	return text[start : to+end]
}

func shannonEntropy(data string) float64 {
	if data == "" {
		return 0
	}
	counts := make(map[rune]int)
	for _, char := range data {
		counts[char]++
	}
	var entropy float64
	inverse := 1.0 / float64(len(data))
	for _, count := range counts {
		frequency := float64(count) * inverse
		entropy -= frequency * math.Log2(frequency)
	}
	return entropy
}

const genericAPIKeyRule = "generic-api-key"

// credentialShaped reports whether a generic-api-key value carries a digit or
// mixed-case letters; prose shaped like an assignment carries neither
// (20261101-021050-d-tac-jac).
func credentialShaped(value string) bool {
	var lower, upper bool
	for _, r := range value {
		switch {
		case unicode.IsDigit(r):
			return true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		}
	}
	return lower && upper
}

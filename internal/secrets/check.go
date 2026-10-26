package secrets

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

var ErrCredential = errors.New("looks like a credential")

// CheckText refuses text holding a credential; where names the part of the
// write it came from, as the user knows it ("body", "summary", ...).
func CheckText(where, text string) error {
	findings, err := Scan(text)
	if err != nil {
		return err
	}
	return refusal(where, findings)
}

// CheckFile refuses a file holding a credential, judged with the rules scoped
// to its name.
func CheckFile(name string, content []byte) error {
	findings, err := ScanFile(name, content)
	if err != nil {
		return err
	}
	return refusal(fileWhere(name), findings)
}

// CheckEntry refuses an entry whose title, summary, body or any other
// frontmatter value holds a credential.
func CheckEntry(entry *model.Entry) error {
	where, findings, err := ScanEntry(entry)
	if err != nil {
		return err
	}
	return refusal(where, findings)
}

// ScanEntry scans an entry part by part, so a finding's line points into the
// part it names, and returns the first part holding any.
func ScanEntry(entry *model.Entry) (string, []Finding, error) {
	rest := *entry
	rest.Title = ""
	rest.Summary = ""
	parts := []struct{ where, text string }{
		{"title", entry.Title},
		{"summary", entry.Summary},
		{"body", entry.Content},
		{"frontmatter", model.FormatFrontmatter(&rest)},
	}
	for _, part := range parts {
		findings, err := Scan(part.text)
		if err != nil {
			return "", nil, err
		}
		if len(findings) > 0 {
			return part.where, findings, nil
		}
	}
	return "", nil, nil
}

// Describe names where a credential was found and by which rules, never the
// credential itself: "the body looks like a credential (github-pat, line 3)".
func Describe(where string, findings []Finding) string {
	line := findings[0].Line
	var rules []string
	for _, finding := range findings {
		if finding.Line == line && !slices.Contains(rules, finding.RuleID) {
			rules = append(rules, finding.RuleID)
		}
	}
	slices.Sort(rules)
	return fmt.Sprintf("the %s %s (%s, line %d)", where, ErrCredential, strings.Join(rules, ", "), line)
}

func fileWhere(name string) string {
	return "attachment " + name
}

func refusal(where string, findings []Finding) error {
	if len(findings) == 0 {
		return nil
	}
	return &refusalError{description: Describe(where, findings)}
}

type refusalError struct{ description string }

func (e *refusalError) Error() string {
	return "refusing to write: " + e.description + "; the graph is shared and immutable"
}

func (e *refusalError) Unwrap() error { return ErrCredential }

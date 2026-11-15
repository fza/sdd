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
	return refusal(Part{Where: where, Text: text}, findings)
}

// CheckFile refuses a file holding a credential, judged with the rules scoped
// to its name.
func CheckFile(name string, content []byte) error {
	findings, err := ScanFile(name, content)
	if err != nil {
		return err
	}
	return refusal(Part{Where: fileWhere(name), Text: string(content)}, findings)
}

// CheckEntry refuses an entry whose summary, body or any other
// frontmatter value holds a credential.
func CheckEntry(entry *model.Entry) error {
	part, findings, err := scanParts(EntryParts(entry))
	if err != nil {
		return err
	}
	return refusal(part, findings)
}

// Part is one piece of text, named as the user knows it ("body", "summary").
type Part struct{ Where, Text string }

// EntryParts splits an entry into its summary, body and each other
// frontmatter field, so a finding's line points into the part it names.
func EntryParts(entry *model.Entry) []Part {
	rest := *entry
	rest.Summary = ""
	parts := []Part{
		{Where: "summary", Text: entry.Summary},
		{Where: "body", Text: entry.Content},
	}
	return append(parts, frontmatterFields(model.FormatFrontmatter(&rest))...)
}

// frontmatterFields cuts rendered frontmatter at each top-level key: the user
// wrote a field, never the frontmatter sdd renders from it. Blank lines stay,
// so a line number inside a block scalar holds.
func frontmatterFields(frontmatter string) []Part {
	var fields []Part
	for _, line := range strings.SplitAfter(frontmatter, "\n") {
		content := strings.TrimRight(line, "\n")
		if content == "---" {
			continue
		}
		key, _, found := strings.Cut(content, ":")
		if found && !strings.HasPrefix(content, " ") && !strings.HasPrefix(content, "-") {
			fields = append(fields, Part{Where: "frontmatter field " + key})
		}
		if len(fields) > 0 {
			fields[len(fields)-1].Text += line
		}
	}
	return fields
}

// ScanEntry scans an entry part by part and returns the first part holding
// any finding.
func ScanEntry(entry *model.Entry) (string, []Finding, error) {
	part, findings, err := scanParts(EntryParts(entry))
	return part.Where, findings, err
}

func scanParts(parts []Part) (Part, []Finding, error) {
	for _, part := range parts {
		findings, err := Scan(part.Text)
		if err != nil {
			return Part{}, nil, err
		}
		if len(findings) > 0 {
			return part, findings, nil
		}
	}
	return Part{}, nil, nil
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

func refusal(part Part, findings []Finding) error {
	if len(findings) == 0 {
		return nil
	}
	return &refusalError{where: part.Where, passage: passageBefore(part.Text, findings), description: Describe(part.Where, findings)}
}

// passageBefore is the text on the first finding's line before its earliest
// match: words that locate the credential without holding it.
func passageBefore(text string, findings []Finding) string {
	line, column := findings[0].Line, findings[0].Column
	for _, finding := range findings {
		if finding.Line == line {
			column = min(column, finding.Column)
		}
	}
	lines := strings.Split(text, "\n")
	if line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[line-1][:column])
}

// RefusedPassage locates what a write refusal refused: the draft's words just
// before the credential, else the part's name, such as "body"; empty when err
// is no write refusal.
func RefusedPassage(err error) string {
	var refusal *refusalError
	if !errors.As(err, &refusal) {
		return ""
	}
	if refusal.passage != "" {
		return refusal.passage
	}
	return refusal.where
}

type refusalError struct{ where, passage, description string }

func (e *refusalError) Error() string {
	return "refusing to write: " + e.description + "; the graph is shared and immutable"
}

func (e *refusalError) Unwrap() error { return ErrCredential }

package presenters

import (
	"fmt"
	"io"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// RenderSecretRules writes which gitleaks rule file credential scans use and
// how the daily fetch went.
func RenderSecretRules(w io.Writer, s model.SecretRulesStatus) {
	fmt.Fprintf(w, "Secret rules: gitleaks %s (%s), updates %s\n", s.Tag, s.Source, s.Update)
	if s.Commit != "" {
		fmt.Fprintf(w, "  Commit: %s\n", s.Commit)
	}
	for _, field := range []struct {
		label string
		at    time.Time
		text  string
	}{
		{"Last accepted fetch", s.LastAccepted, ""},
		{"Last check", s.LastChecked, ""},
		{"Last failure", s.LastFailureAt, s.LastFailure},
	} {
		if field.at.IsZero() {
			continue
		}
		line := "  " + field.label + ": " + field.at.Local().Format(time.RFC3339)
		if field.text != "" {
			line += ": " + field.text
		}
		fmt.Fprintln(w, line)
	}
	if s.Unused != "" {
		fmt.Fprintf(w, "  Fetched copy not used: %s\n", s.Unused)
	}
}

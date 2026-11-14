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

// secretRulesJSON is the secret rules status in sdd's JSON outputs.
type secretRulesJSON struct {
	Source       string            `json:"source"`
	Tag          string            `json:"tag"`
	Update       string            `json:"update"`
	Unused       string            `json:"unused,omitempty"`
	LastAccepted string            `json:"last_accepted,omitempty"`
	LastChecked  string            `json:"last_checked,omitempty"`
	LastFailure  *secretRulesError `json:"last_failure,omitempty"`
}

// secretRulesError is a failed or rejected fetch.
type secretRulesError struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

// secretRulesValue renders s for a JSON output.
func secretRulesValue(s model.SecretRulesStatus) secretRulesJSON {
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	value := secretRulesJSON{
		Source: s.Source, Tag: s.Tag, Update: string(s.Update), Unused: s.Unused,
		LastAccepted: stamp(s.LastAccepted), LastChecked: stamp(s.LastChecked),
	}
	if !s.LastFailureAt.IsZero() {
		value.LastFailure = &secretRulesError{At: stamp(s.LastFailureAt), Reason: s.LastFailure}
	}
	return value
}

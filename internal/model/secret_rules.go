package model

import "time"

// Where the credential rules in use came from.
const (
	SecretRulesBuiltIn = "built-in"
	SecretRulesFetched = "fetched"
)

// SecretRulesStatus is what sdd info reports about the credential rules scans
// use.
type SecretRulesStatus struct {
	// Update is the effective secrets.rules_update.
	Update SecretRulesUpdate
	// Source is SecretRulesBuiltIn or SecretRulesFetched; Tag is the gitleaks
	// release its rule file came from.
	Source string
	Tag    string
	// Unused says why a fetched copy on disk is not in use.
	Unused        string
	LastAccepted  time.Time
	LastChecked   time.Time
	LastFailure   string
	LastFailureAt time.Time
}

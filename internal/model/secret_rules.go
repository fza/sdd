package model

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// SecretsConfig holds the credential scanner's settings, read from the
// user-global config only.
type SecretsConfig struct {
	// RulesUpdate decides whether the daily pass fetches gitleaks' newest
	// released rule file. Empty means DefaultSecretRulesUpdate.
	RulesUpdate SecretRulesUpdate `yaml:"rules_update,omitempty"`
}

// SecretRulesUpdate is the secrets.rules_update setting. Off also sets aside
// a copy fetched earlier: scans use the built-in rule file alone.
type SecretRulesUpdate string

const (
	SecretRulesDaily SecretRulesUpdate = "daily"
	SecretRulesOff   SecretRulesUpdate = "off"

	DefaultSecretRulesUpdate = SecretRulesDaily

	secretRulesUpdateKey = "secrets.rules_update"
)

func (SecretRulesUpdate) AllowedValues() []string {
	return closedValues(SecretRulesDaily, SecretRulesOff)
}

// ErrUnknownSecretRulesUpdate reports a secrets.rules_update value sdd does
// not know.
var ErrUnknownSecretRulesUpdate = errors.New("unknown " + secretRulesUpdateKey)

// Validate refuses a value other than the AllowedValues; empty is unset. The
// user-global config carrying one fails to load: read past, the daily pass
// would fetch rule files the person may have meant to turn off.
func (u SecretRulesUpdate) Validate() error {
	if u == "" || slices.Contains(u.AllowedValues(), string(u)) {
		return nil
	}
	return fmt.Errorf("%w: %q is not %s", ErrUnknownSecretRulesUpdate, u, oneOf(u.AllowedValues()))
}

// ResolveSecretRulesUpdate returns the effective secrets.rules_update. A value
// sdd does not know falls back to the default, as an unset one does; the
// global config load refuses one before it gets here.
func ResolveSecretRulesUpdate(cfg SecretsConfig) SecretRulesUpdate {
	if cfg.RulesUpdate == "" || cfg.RulesUpdate.Validate() != nil {
		return DefaultSecretRulesUpdate
	}
	return cfg.RulesUpdate
}

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
	// Commit is the gitleaks commit the rule file was read at.
	Commit string
	// Unused says why a fetched copy on disk is not in use.
	Unused        string
	LastAccepted  time.Time
	LastChecked   time.Time
	LastFailure   string
	LastFailureAt time.Time
}

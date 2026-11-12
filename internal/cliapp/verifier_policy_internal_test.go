package cliapp

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

func TestVerifierPolicyCarriesTheAgentBlocks(t *testing.T) {
	cfg := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Verify: model.VerifyConfig{Claude: model.VerifierAgent{Model: "opus", Effort: "high"}}}}

	policy := verifierPolicy(cfg)

	if policy.Claude != (sdd.VerifierAgent{Model: "opus", Effort: "high"}) {
		t.Errorf("the application policy should carry verify.claude, got %+v", policy.Claude)
	}
	if policy.Codex != (sdd.VerifierAgent{Effort: "medium"}) {
		t.Errorf("the application policy should carry the resolved verify.codex, got %+v", policy.Codex)
	}
}

// A project without any config resolves to the defaults, prose included.
func TestVerifierPolicyWithoutConfigResolvesTheDefaults(t *testing.T) {
	policy := verifierPolicy(nil)

	if policy.Mode != string(model.VerifierProse) || policy.Runner != string(model.VerifierRunnerSubagent) || policy.Claude != (sdd.VerifierAgent{Model: "sonnet", Effort: "medium"}) {
		t.Fatalf("no config should resolve to prose, the subagent runner and the default agents, got %+v", policy)
	}
}

func TestVerifierPolicyCarriesTheRoundTimeout(t *testing.T) {
	cfg := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Verify: model.VerifyConfig{RoundTimeout: "90s"}}}

	if got := verifierPolicy(cfg).RoundTimeout; got != 90*time.Second {
		t.Fatalf("the application policy should carry verify.round_timeout, got %s", got)
	}
}

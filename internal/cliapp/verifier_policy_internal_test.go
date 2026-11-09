package cliapp

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

func TestVerifierPolicyCarriesTheSubagentBlock(t *testing.T) {
	cfg := &model.PerRepoConfig{BaseConfig: model.BaseConfig{Verifier: model.VerifierConfig{Subagent: model.VerifierAgent{Model: "opus", Effort: "high"}}}}

	policy := verifierPolicy(cfg)

	if policy.Subagent != (sdd.VerifierAgent{Model: "opus", Effort: "high"}) {
		t.Fatalf("the application policy should carry verifier.subagent, got %+v", policy.Subagent)
	}
}

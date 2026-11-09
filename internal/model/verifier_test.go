package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

var verifierFiles = map[model.ConfigValueSource]string{
	model.ConfigSourceGlobal:  "global.yaml",
	model.ConfigSourceProject: ".sdd/config.yaml",
	model.ConfigSourceLocal:   ".sdd/config.local.yaml",
}

func verifierLayer(mode model.VerifierMode, skip model.VerifierSkip, enforce *bool) *model.PerRepoConfig {
	cfg := &model.PerRepoConfig{}
	cfg.Verifier = model.VerifierConfig{Mode: mode, Skip: skip}
	cfg.Grounding.Enforce = enforce
	return cfg
}

func boolPtr(v bool) *bool { return &v }

func TestCheckVerifierLayers_ModeRequiresGroundingEnforcement(t *testing.T) {
	err := model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer(model.VerifierLight, "", nil), nil, verifierFiles)
	if err == nil {
		t.Fatal("light without grounding enforcement should be refused")
	}
	for _, want := range []string{"verifier.mode light", "set in .sdd/config.yaml", "grounding.enforce", "unset, off by default"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal should name %q: %v", want, err)
		}
	}

	global := model.BaseConfig{}
	global.Grounding.Enforce = boolPtr(false)
	err = model.CheckVerifierLayers(global, verifierLayer(model.VerifierDeep, "", nil), nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), "false in global.yaml") {
		t.Fatalf("the refusal should name the layer that set enforcement off, got %v", err)
	}

	err = model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer(model.VerifierDeep, "", boolPtr(true)), nil, verifierFiles)
	if err != nil {
		t.Fatalf("deep with enforcement on resolves: %v", err)
	}
	err = model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer(model.VerifierOff, "", nil), nil, verifierFiles)
	if err != nil {
		t.Fatalf("off needs no enforcement: %v", err)
	}
}

func TestCheckVerifierLayers_LocalMayOnlyTighten(t *testing.T) {
	committed := verifierLayer(model.VerifierDeep, model.VerifierSkipFailure, boolPtr(true))
	for _, tc := range []struct {
		name  string
		local *model.PerRepoConfig
		want  string
	}{
		{"lower mode", verifierLayer(model.VerifierLight, "", nil), "verifier.mode light is refused in the machine-local config"},
		{"looser skip", verifierLayer("", model.VerifierSkipWeighed, nil), "verifier.skip weighed is refused in the machine-local config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := model.CheckVerifierLayers(model.BaseConfig{}, committed, tc.local, verifierFiles)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), ".sdd/config.local.yaml") || !strings.Contains(err.Error(), "in .sdd/config.yaml") {
				t.Fatalf("want a refusal naming %q and both files, got %v", tc.want, err)
			}
		})
	}

	raised := model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer(model.VerifierLight, model.VerifierSkipFailure, boolPtr(true)), verifierLayer(model.VerifierDeep, model.VerifierSkipNever, nil), verifierFiles)
	if raised != nil {
		t.Fatalf("a local layer raising mode and tightening skip resolves: %v", raised)
	}
}

func TestCheckVerifierLayers_CommittedWinsOverGlobal(t *testing.T) {
	global := model.BaseConfig{}
	global.Verifier.Mode = model.VerifierDeep
	global.Grounding.Enforce = boolPtr(true)
	committed := verifierLayer(model.VerifierLight, "", nil)
	merged := model.MergeConfig(&model.PerRepoConfig{BaseConfig: global}, committed)
	if merged.Verifier.Policy().Mode != model.VerifierLight {
		t.Fatalf("the committed mode should win over the global one, got %s", merged.Verifier.Policy().Mode)
	}
	err := model.CheckVerifierLayers(global, committed, nil, verifierFiles)
	if err != nil {
		t.Fatalf("committed light over global deep resolves: %v", err)
	}
	err = model.CheckVerifierLayers(global, committed, verifierLayer(model.VerifierLight, "", nil), verifierFiles)
	if err != nil {
		t.Fatalf("a local value equal to the committed one resolves: %v", err)
	}
}

func TestCheckVerifierLayers_RefusesUnknownValues(t *testing.T) {
	err := model.CheckVerifierLayers(model.BaseConfig{}, verifierLayer("strict", "", boolPtr(true)), nil, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.yaml: verifier.mode "strict"`) {
		t.Fatalf("an unknown mode should be refused naming file and key, got %v", err)
	}
	err = model.CheckVerifierLayers(model.BaseConfig{}, nil, verifierLayer("", "sometimes", nil), verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `.sdd/config.local.yaml: verifier.skip "sometimes"`) {
		t.Fatalf("an unknown skip should be refused naming file and key, got %v", err)
	}
}

func TestVerifierPolicyDefaults(t *testing.T) {
	policy := model.VerifierConfig{}.Policy()
	if policy.Mode != model.VerifierOff || policy.Skip != model.VerifierSkipFailure {
		t.Fatalf("defaults are off and failure, got %+v", policy)
	}
}

func TestStrictnessSetsBothKeysAsAValidPair(t *testing.T) {
	for _, s := range model.Strictnesses() {
		enforce, mode := s.Settings()
		cfg := &model.PerRepoConfig{}
		cfg.Grounding.Enforce = &enforce
		cfg.Verifier.Mode = mode
		err := model.CheckVerifierLayers(model.BaseConfig{}, cfg, nil, verifierFiles)
		if err != nil {
			t.Errorf("strictness %s should set a pair that resolves: %v", s, err)
		}
	}
	enforce, mode := model.StrictnessGrounding.Settings()
	if !enforce || mode != model.VerifierOff {
		t.Fatalf("grounding enforces without a verifier, got %v %s", enforce, mode)
	}
	_, err := model.ParseStrictness("strict")
	if err == nil {
		t.Fatal("an answer outside the set should be refused")
	}
}

package model_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestVerifyConfig_ModeForAppliesTheBuiltInCapsWhileNoKindIsNamed(t *testing.T) {
	cases := []struct {
		name  string
		block model.VerifyConfig
		kind  model.Kind
		want  model.VerifyMode
	}{
		{"capped kind under deep", model.VerifyConfig{Mode: model.VerifyDeep}, "done", model.VerifyProse},
		{"uncapped kind under deep", model.VerifyConfig{Mode: model.VerifyDeep}, "directive", model.VerifyDeep},
		{"capped kind under a lower mode", model.VerifyConfig{Mode: model.VerifyGrounding}, "actor", model.VerifyGrounding},
		{"unset mode", model.VerifyConfig{}, "insight", model.DefaultVerifyMode},
		{"off", model.VerifyConfig{Mode: model.VerifyOff}, "directive", model.VerifyOff},
		{"an empty map names no kind", model.VerifyConfig{Mode: model.VerifyDeep, Kinds: map[model.Kind]model.VerifyMode{}}, "procedure", model.VerifyProse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.block.ModeFor(tc.kind); got != tc.want {
				t.Fatalf("the kind should resolve to the expected mode, got %q", got)
			}
		})
	}
	for _, kind := range []model.Kind{"done", "actor", "role", "focus", "annotation", "insight", "question", "activity", "procedure"} {
		if got := (model.VerifyConfig{Mode: model.VerifyDeep}).ModeFor(kind); got != model.VerifyProse {
			t.Errorf("%s should be capped at prose under deep, got %q", kind, got)
		}
	}
	for _, kind := range []model.Kind{"gap", "fact", "directive", "plan", "contract", "aspiration"} {
		if got := (model.VerifyConfig{Mode: model.VerifyDeep}).ModeFor(kind); got != model.VerifyDeep {
			t.Errorf("%s should keep deep, got %q", kind, got)
		}
	}
}

func TestVerifyConfig_ModeForTakesANamedKindExactly(t *testing.T) {
	block := model.VerifyConfig{Mode: model.VerifyProse, Kinds: map[model.Kind]model.VerifyMode{"done": model.VerifyDeep, "fact": model.VerifyOff}}
	for kind, want := range map[model.Kind]model.VerifyMode{
		"done":     model.VerifyDeep,
		"fact":     model.VerifyOff,
		"insight":  model.VerifyProse,
		"activity": model.VerifyProse,
	} {
		if got := block.ModeFor(kind); got != want {
			t.Errorf("%s should resolve to %s once a kind is named, got %q", kind, want, got)
		}
	}
	unknown := model.VerifyConfig{Mode: model.VerifyDeep, Kinds: map[model.Kind]model.VerifyMode{"done": "deeep"}}
	if got := unknown.ModeFor("done"); got != model.VerifyProse {
		t.Errorf("a kind with an unknown mode should count as unnamed, leaving the built-in cap, got %q", got)
	}
}

func TestVerifyConfig_StricterKindsOnlyForAKindLooserThanAnother(t *testing.T) {
	block := model.VerifyConfig{Mode: model.VerifyProse, Kinds: map[model.Kind]model.VerifyMode{"insight": model.VerifyOff, "contract": model.VerifyDeep}}
	got := block.StricterKinds("insight")
	if len(got) != 14 || got[0] != "gap" {
		t.Fatalf("an off kind under prose should list every other kind as stricter, got %v", got)
	}
	if got := block.StricterKinds("contract"); got != nil {
		t.Fatalf("the strictest kind needs no kind-fit check, got %v", got)
	}
	if got := (model.VerifyConfig{Mode: model.VerifyDeep}).StricterKinds("gap"); got != nil {
		t.Fatalf("a kind at the mode with no kind above it needs no kind-fit check, got %v", got)
	}
	capped := model.VerifyConfig{Mode: model.VerifyDeep}.StricterKinds("done")
	if len(capped) != 6 {
		t.Fatalf("a kind capped at prose under deep should list the six uncapped kinds, got %v", capped)
	}
}

func TestVerifyConfig_StricterKindsForAKindAtTheModeBelowAConfiguredKind(t *testing.T) {
	block := model.VerifyConfig{Mode: model.VerifyProse, Kinds: map[model.Kind]model.VerifyMode{"directive": model.VerifyDeep}}
	if got := block.StricterKinds("done"); !slices.Equal(got, []model.Kind{"directive"}) {
		t.Fatalf("a kind at verify.mode should be tested against the kind configured stricter than it, got %v", got)
	}
	if got := block.StricterKinds("directive"); got != nil {
		t.Fatalf("the strictest kind needs no kind-fit check, got %v", got)
	}
}

func TestMergeBaseConfig_VerifyKindsMergeKeyByKey(t *testing.T) {
	var global, committed model.BaseConfig
	global.Verify.Kinds = map[model.Kind]model.VerifyMode{"done": model.VerifyDeep, "fact": model.VerifyOff}
	committed.Verify.Kinds = map[model.Kind]model.VerifyMode{"fact": model.VerifyProse}
	merged := model.MergeBaseConfig(global, committed).Verify
	if merged.Kinds["done"] != model.VerifyDeep || merged.Kinds["fact"] != model.VerifyProse {
		t.Fatalf("each kind should take the upper layer's value where it sets one, got %v", merged.Kinds)
	}
	if global.Verify.Kinds["fact"] != model.VerifyOff {
		t.Fatal("the merge should leave the lower layer's map unchanged")
	}
}

func TestCheckVerifyLayers_LocalMayOnlyRaiseEachKind(t *testing.T) {
	committed := &model.PerRepoConfig{}
	committed.Verify = model.VerifyConfig{Mode: model.VerifyProse, Kinds: map[model.Kind]model.VerifyMode{"done": model.VerifyDeep}}
	for _, tc := range []struct {
		name  string
		local model.VerifyConfig
		want  string
	}{
		{"lowers a named kind", model.VerifyConfig{Kinds: map[model.Kind]model.VerifyMode{"done": model.VerifyProse}}, "verify.kinds.done prose"},
		{"lowers an unnamed kind", model.VerifyConfig{Kinds: map[model.Kind]model.VerifyMode{"gap": model.VerifyGrounding}}, "verify.kinds.gap grounding"},
		{"raises a kind", model.VerifyConfig{Kinds: map[model.Kind]model.VerifyMode{"gap": model.VerifyDeep}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := &model.PerRepoConfig{}
			local.Verify = tc.local
			err := model.CheckVerifyLayers(model.BaseConfig{}, committed, local, verifierFiles)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("a machine-local kind raising its mode should pass: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), ".sdd/config.local.yaml") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("a machine-local kind lowering its mode should be refused naming the file and kind, got %v", err)
			}
		})
	}
	var global model.BaseConfig
	global.Verify.Mode = model.VerifyDeep
	local := &model.PerRepoConfig{}
	local.Verify.Kinds = map[model.Kind]model.VerifyMode{"gap": model.VerifyDeep}
	err := model.CheckVerifyLayers(global, nil, local, verifierFiles)
	if err != nil {
		t.Fatalf("naming a kind locally turns the built-in caps off, which only raises the capped kinds: %v", err)
	}
}

func TestIgnoredSettings_ReportsUnknownVerifyKinds(t *testing.T) {
	var layer model.BaseConfig
	layer.Verify.Kinds = map[model.Kind]model.VerifyMode{"done": "deeep", "gapp": model.VerifyDeep}
	reasons := map[string]string{}
	for _, s := range model.IgnoredSettings(layer, model.ConfigSourceProject) {
		reasons[s.Key] = s.Reason
	}
	if !strings.Contains(reasons["verify.kinds.done"], `"deeep" is not`) {
		t.Errorf("an unknown mode for a kind should be reported, got %q", reasons["verify.kinds.done"])
	}
	if !strings.Contains(reasons["verify.kinds.gapp"], "not an entry kind") {
		t.Errorf("a name that is no entry kind should be reported, got %q", reasons["verify.kinds.gapp"])
	}
}

func TestVerifyKeyRefusal_RefusesAnUnknownKindOnWrite(t *testing.T) {
	local := &model.PerRepoConfig{}
	local.Verify.Kinds = map[model.Kind]model.VerifyMode{"gapp": model.VerifyDeep}
	err := model.VerifyKeyRefusal("verify.kinds.gapp", model.BaseConfig{}, nil, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), "not an entry kind") {
		t.Fatalf("a write should refuse a name that is no entry kind, got %v", err)
	}
	local.Verify.Kinds = map[model.Kind]model.VerifyMode{"gap": "deeep"}
	err = model.VerifyKeyRefusal("verify.kinds.gap", model.BaseConfig{}, nil, local, verifierFiles)
	if err == nil || !strings.Contains(err.Error(), `"deeep" is not`) {
		t.Fatalf("a write should refuse an unknown mode for a kind, got %v", err)
	}
}

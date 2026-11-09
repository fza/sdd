package model_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func stepEvent(procedure, step, phase string, findings bool) model.HookEvent {
	return model.HookEvent{Name: model.StepHookEvent(procedure, step, phase), Procedure: procedure, Step: step, Phase: phase, Findings: findings}
}

var raisedEvents = []model.HookEvent{
	stepEvent("capture", "assemble", model.HookPhaseStart, false),
	stepEvent("capture", "assemble", model.HookPhaseDone, true),
	stepEvent("capture", "playback", model.HookPhaseDone, false),
	stepEvent("capture", "verify", model.HookPhaseRound, false),
	stepEvent("explore", "frame", model.HookPhaseDone, false),
	{Name: "entry.written"},
}

func hook(event string, findings bool) model.Hook {
	return model.Hook{HookConfig: model.HookConfig{Event: event, Command: "lint", Findings: findings}, Origin: model.HookOrigin{ConfigPath: ".sdd/config.yaml"}}
}

func TestStepHookEventNamesTheDoneEventBare(t *testing.T) {
	for phase, want := range map[string]string{model.HookPhaseDone: "capture.assemble", model.HookPhaseStart: "capture.assemble.start", model.HookPhaseRound: "capture.assemble.round"} {
		if got := model.StepHookEvent("capture", "assemble", phase); got != want {
			t.Errorf("StepHookEvent(%s) = %q, want %q", phase, got, want)
		}
	}
}

func TestMatchHookEvent(t *testing.T) {
	assembleDone := stepEvent("capture", "assemble", model.HookPhaseDone, true)
	assembleStart := stepEvent("capture", "assemble", model.HookPhaseStart, false)
	frameDone := stepEvent("explore", "frame", model.HookPhaseDone, false)
	written := model.HookEvent{Name: "entry.written"}
	for _, tc := range []struct {
		pattern string
		event   model.HookEvent
		want    bool
	}{
		{"capture.assemble", assembleDone, true},
		{"capture.assemble.done", assembleDone, true},
		{"capture.assemble", assembleStart, false},
		{"capture.assemble.start", assembleStart, true},
		{"capture.*", assembleDone, true},
		{"*.assemble", assembleDone, true},
		{"*.*", frameDone, true},
		{"*.*.done", frameDone, true},
		{"*.*.start", assembleStart, true},
		{"*.*.start", assembleDone, false},
		{"capture.*", frameDone, false},
		{"capture.assemble.*", assembleDone, false},
		{"*.*", written, false},
		{"entry.written", written, true},
		{"entry.written.done", written, false},
		{"*.*.", written, false},
		{"*", assembleDone, false},
	} {
		if got := model.MatchHookEvent(tc.pattern, tc.event); got != tc.want {
			t.Errorf("MatchHookEvent(%q, %q) = %v, want %v", tc.pattern, tc.event.Name, got, tc.want)
		}
	}
}

func TestValidateHooksRefusesAnUnknownEventListingTheValidOnes(t *testing.T) {
	err := model.ValidateHooks([]model.Hook{hook("capture.asemble", false)}, raisedEvents)
	if err == nil {
		t.Fatal("an unknown event validated")
	}
	for _, want := range []string{`.sdd/config.yaml: hooks[0]`, `unknown event "capture.asemble"`, "capture.assemble.start", "entry.written"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not carry %q", err, want)
		}
	}
}

func TestValidateHooksAcceptsWildcardsAndAliasesMatchingARaisedEvent(t *testing.T) {
	hooks := []model.Hook{hook("*.*", false), hook("capture.*.start", false), hook("entry.written", false), hook("capture.assemble.done", true), hook("capture.verify.round", false)}
	if err := model.ValidateHooks(hooks, raisedEvents); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHooksRefusesFindingsOffAFindingsChannel(t *testing.T) {
	err := model.ValidateHooks([]model.Hook{hook("capture.*", true)}, raisedEvents)
	if err == nil {
		t.Fatal("findings on a step without a findings channel validated")
	}
	if !strings.Contains(err.Error(), "capture.playback has no findings channel") || !strings.Contains(err.Error(), "events with one: capture.assemble") {
		t.Errorf("refusal %q does not name the step without a channel and the ones with one", err)
	}
}

func TestValidateHooksRefusesANegativeTimeout(t *testing.T) {
	negative := hook("entry.written", false)
	negative.Timeout = -1
	err := model.ValidateHooks([]model.Hook{negative}, raisedEvents)
	if err == nil || !strings.Contains(err.Error(), "timeout -1 is negative") {
		t.Fatalf("refusal = %v, want the negative timeout named", err)
	}
}

func TestHookTimeoutDefaultsTo600Seconds(t *testing.T) {
	if got := (model.HookConfig{}).TimeoutDuration(); got != 600*time.Second {
		t.Errorf("default timeout = %s, want 600s", got)
	}
	if got := (model.HookConfig{Timeout: 5}).TimeoutDuration(); got != 5*time.Second {
		t.Errorf("timeout = %s, want 5s", got)
	}
}

func TestHookCommandPathNamesAFileInTheHooksFolder(t *testing.T) {
	for command, want := range map[string]string{
		"vale.sh":             "vale.sh",
		"lint/vale.sh":        "lint/vale.sh",
		"./vale.sh":           "vale.sh",
		`my\ lint.sh`:         "my lint.sh",
		"local/mine.sh":       filepath.Join("local", "mine.sh"),
		"tools/./../tools/x":  "",
		"local/../escape.sh":  "",
		"vale-v3.0_final.py":  "vale-v3.0_final.py",
		"nested/dir/check.rb": filepath.Join("nested", "dir", "check.rb"),
	} {
		got, err := model.HookCommandPath(command, model.HookLayerLocal)
		if want == "" {
			if err == nil {
				t.Errorf("HookCommandPath(%q) = %q, want a refusal", command, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("HookCommandPath(%q) = %q, %v; want %q", command, got, err, want)
		}
	}
}

func TestHookCommandPathRefusesWhatAShellOrPathWouldResolve(t *testing.T) {
	for command, reason := range map[string]string{
		"":                      "command is empty",
		"vale --config x":       "holds whitespace",
		"cat > payload.json":    "holds whitespace",
		"lint;rm":               `shell character ';'`,
		"$HOME/lint":            `shell character '$'`,
		"lint|tee":              `shell character '|'`,
		"/usr/bin/vale":         "is an absolute path",
		"../bin/vale":           "leaves the hooks folder",
		"tools/../../bin/vale":  "leaves the hooks folder",
		`lint\x`:                "escapes only a space",
		`lint\`:                 "ends in a backslash",
		"lint\tx":               "holds whitespace",
		"~/bin/vale":            `shell character '~'`,
		"vale\\ --config\\ \"x": `shell character '"'`,
	} {
		_, err := model.HookCommandPath(command, model.HookLayerLocal)
		if err == nil || !strings.Contains(err.Error(), reason) {
			t.Errorf("HookCommandPath(%q) = %v, want a refusal naming %q", command, err, reason)
		}
	}
}

func TestHookCommandPathRefusesACommittedHookIntoLocal(t *testing.T) {
	for _, command := range []string{"local/mine.sh", "./local/mine.sh", "local"} {
		_, err := model.HookCommandPath(command, model.HookLayerCommitted)
		if err == nil || !strings.Contains(err.Error(), "config.local.yaml") {
			t.Errorf("committed HookCommandPath(%q) = %v, want a refusal naming config.local.yaml", command, err)
		}
		if _, err := model.HookCommandPath(command, model.HookLayerLocal); err != nil {
			t.Errorf("a local hook may run %q, got %v", command, err)
		}
	}
	if _, err := model.HookCommandPath("localized.sh", model.HookLayerCommitted); err != nil {
		t.Errorf("a committed hook may run a file merely starting with local, got %v", err)
	}
}

func TestOnlyTheUserGlobalLayerRunsUnconfirmed(t *testing.T) {
	for layer, want := range map[model.HookLayer]bool{model.HookLayerGlobal: false, model.HookLayerCommitted: true, model.HookLayerLocal: true} {
		if got := (model.HookOrigin{Layer: layer}).NeedsConfirmation(); got != want {
			t.Errorf("%s NeedsConfirmation = %v, want %v", layer, got, want)
		}
	}
}

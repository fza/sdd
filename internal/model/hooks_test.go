package model_test

import (
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

var raisedEvents = []model.HookEvent{
	{Name: "capture.assemble.entered"},
	{Name: "capture.assemble.left", Findings: true},
	{Name: "capture.playback.left"},
	{Name: "explore.frame.left"},
	{Name: "entry.written"},
}

func TestMatchHookEvent(t *testing.T) {
	for _, tc := range []struct {
		pattern, event string
		want           bool
	}{
		{"capture.assemble.left", "capture.assemble.left", true},
		{"capture.*.left", "capture.playback.left", true},
		{"*.assemble.left", "capture.assemble.left", true},
		{"*.*.left", "explore.frame.left", true},
		{"capture.*.left", "explore.frame.left", false},
		{"capture.assemble.*", "capture.assemble.left", false},
		{"*.*.left", "entry.written", false},
		{"entry.written", "entry.written", true},
		{"*", "capture.assemble.left", false},
	} {
		if got := model.MatchHookEvent(tc.pattern, tc.event); got != tc.want {
			t.Errorf("MatchHookEvent(%q, %q) = %v, want %v", tc.pattern, tc.event, got, tc.want)
		}
	}
}

func TestValidateHooksRefusesAnUnknownEventListingTheValidOnes(t *testing.T) {
	err := model.ValidateHooks([]model.HookConfig{{Event: "capture.asemble.left", Command: "lint"}}, raisedEvents)
	if err == nil {
		t.Fatal("an unknown event validated")
	}
	for _, want := range []string{`unknown event "capture.asemble.left"`, "capture.assemble.entered", "entry.written"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not carry %q", err, want)
		}
	}
}

func TestValidateHooksAcceptsWildcardsMatchingARaisedEvent(t *testing.T) {
	hooks := []model.HookConfig{
		{Event: "*.*.left", Command: "status"},
		{Event: "capture.*.entered", Command: "status"},
		{Event: "entry.written", Command: "notify"},
		{Event: "capture.assemble.left", Command: "lint", Findings: true},
	}
	if err := model.ValidateHooks(hooks, raisedEvents); err != nil {
		t.Fatal(err)
	}
}

func TestValidateHooksRefusesFindingsOffAFindingsChannel(t *testing.T) {
	err := model.ValidateHooks([]model.HookConfig{{Event: "capture.*.left", Command: "lint", Findings: true}}, raisedEvents)
	if err == nil {
		t.Fatal("findings on a step without a findings channel validated")
	}
	if !strings.Contains(err.Error(), "capture.playback.left has no findings channel") || !strings.Contains(err.Error(), "events with one: capture.assemble.left") {
		t.Errorf("refusal %q does not name the step without a channel and the ones with one", err)
	}
}

func TestValidateHooksRefusesAnEmptyCommandAndANegativeTimeout(t *testing.T) {
	err := model.ValidateHooks([]model.HookConfig{{Event: "entry.written", Command: " ", Timeout: -1}}, raisedEvents)
	if err == nil || !strings.Contains(err.Error(), "command is empty") || !strings.Contains(err.Error(), "timeout -1 is negative") {
		t.Fatalf("refusal = %v, want the empty command and the negative timeout named", err)
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

package proctest_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/test"
)

// A draft holding a credential reaches no model: the writing guide and the
// write gate each report the refusal as a finding, and revising the draft
// lets the capture go on.
func TestCapture_CredentialRoutesToRevise(t *testing.T) {
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/github-pat.txt")
	if err != nil {
		t.Fatal(err)
	}
	world, session := newCaptureWorld(t, "core-credential")
	draft := captureDraft()
	draft["body"] = "A tactical gap: the fixture observes " + strings.TrimSpace(string(content)) + "."

	serve := session.Start(t, "capture", nil)
	instance := serve.Instance
	serve = session.Report(t, instance, draft)
	proctest.RequireStep(t, serve, "guideReview")
	if !strings.Contains(serve.Instructions, "looks like a credential") {
		t.Errorf("guideReview should name the credential refusal:\n%s", serve.Instructions)
	}
	serve = session.Answer(t, instance, "guideReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "reviseOrOverride")
	if !strings.Contains(serve.Instructions, "looks like a credential") {
		t.Errorf("reviseOrOverride should name the credential refusal:\n%s", serve.Instructions)
	}
	if world.LLM.Calls("writing-guide") != 0 || world.LLM.Calls("preflight") != 0 {
		t.Fatal("a draft holding a credential must reach no model")
	}

	serve = session.Answer(t, instance, "reviseOrOverride", "revise", map[string]any{"body": "A tactical gap: the fixture observes something."}, "drop the token")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "verifySummary")
	if world.LLM.Calls("preflight") != 1 {
		t.Error("the revised draft should pass the pre-flight")
	}
}

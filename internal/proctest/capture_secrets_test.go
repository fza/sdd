package proctest_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/proctest"
	"github.com/networkteam/sdd/test"
)

// A draft holding a credential reaches no model: the writing guide and the
// write gate each report the refusal as a finding, an override sends the
// draft back to revise, a hold for the person is refused, and revising it
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
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	proctest.RequireStep(t, serve, "guideReview")
	if !strings.Contains(serve.Instructions, "looks like a credential") {
		t.Errorf("guideReview should name the credential refusal:\n%s", serve.Instructions)
	}
	if !strings.Contains(serve.Instructions, "credential on 'A tactical gap: the fixture observes export'") {
		t.Errorf("the guide finding should quote the words before the credential:\n%s", serve.Instructions)
	}
	serve = session.Answer(t, instance, "guideReview", "proceed", nil, "")
	proctest.RequireStep(t, serve, "preflightRoute")
	if !strings.Contains(serve.Instructions, "looks like a credential") || !strings.Contains(serve.Instructions, "an override cannot lift it") {
		t.Errorf("preflightRoute should name the credential refusal and that an override cannot lift it:\n%s", serve.Instructions)
	}

	if _, err := session.AnswerErr(t, instance, "preflightRoute", "hold", nil, ""); err == nil || !strings.Contains(err.Error(), "an override cannot lift a credential") {
		t.Fatalf("holding a credential refusal for the person should be refused, got %v", err)
	}
	if world.LLM.Calls("writing-guide") != 0 || world.LLM.Calls("preflight") != 0 {
		t.Fatal("a draft holding a credential must reach no model")
	}

	serve = session.Answer(t, instance, "preflightRoute", "revise", map[string]any{"body": "A tactical gap: the fixture observes something."}, "")
	proctest.RequireStep(t, serve, "playback")
	serve = session.Answer(t, instance, "playback", "confirm", nil, "capture it")
	if serve.Step == "guideReview" {
		serve = session.Answer(t, instance, "guideReview", "proceed", nil, "")
	}
	proctest.RequireStep(t, serve, "verifySummary")
	if world.LLM.Calls("preflight") != 1 {
		t.Error("the revised draft should pass the pre-flight")
	}
}

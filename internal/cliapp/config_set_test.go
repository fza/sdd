package cliapp_test

import (
	"strings"
	"testing"
)

func TestConfigSetEmbeddingPath(t *testing.T) {
	f := newAppFixture(t)
	if r := f.run(t, "config", "set", "embedding.path", "/v3/compat/embeddings"); r.err != nil {
		t.Errorf("a request path should be accepted: %v\n%s", r.err, r.stderr)
	}
	if r := f.run(t, "config", "set", "--local", "embedding.path", "https://gateway.example/v3/compat/embeddings"); r.err == nil || !strings.Contains(r.err.Error(), "embedding.path") {
		t.Error("a URL in embedding.path should be refused")
	}
}

// One machine-wide sweep collects every project's sessions, so only the
// user-global config sets sessions.retention.
func TestSessionRetentionComesFromTheGlobalConfigOnly(t *testing.T) {
	f := newAppFixture(t)
	if r := f.run(t, "config", "set", "sessions.retention", "30d"); r.err != nil {
		t.Errorf("the global config should take the session retention: %v\n%s", r.err, r.stderr)
	}
	if r := f.run(t, "config", "set", "--local", "sessions.retention", "30d"); r.err == nil || !strings.Contains(r.err.Error(), "only the user-global config") {
		t.Errorf("a per-repo session retention should be refused, got %v", r.err)
	}
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nsessions:\n  retention: 30d\n")
	r := f.run(t, "show", appEntryID)
	if r.err != nil {
		t.Fatalf("show: %v\n%s", r.err, r.stderr)
	}
	assertContainsOutput(t, "stderr", r.stderr, "ignoring config setting", "sessions.retention", "only the user-global config")
	listing := f.run(t, "config")
	assertContainsOutput(t, "stderr", listing.stderr, "not honored", "sessions.retention")
}

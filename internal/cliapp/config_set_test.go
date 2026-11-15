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

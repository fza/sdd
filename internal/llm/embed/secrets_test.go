package embed_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llm/embed"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	pkgembed "github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/sdd/test"
)

// The embedder the factory builds refuses a credential before any transport:
// the endpoint below accepts no connection, so reaching it would surface as a
// connection error instead of the refusal.
func TestEmbedderRefusesACredentialBeforeTheProvider(t *testing.T) {
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/stripe-access-token.txt")
	if err != nil {
		t.Fatal(err)
	}
	embedder, err := embed.New(model.EmbeddingConfig{Provider: "ollama", Model: "nomic-embed-text", OllamaEndpoint: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = embedder.Embed(t.Context(), pkgembed.Request{Purpose: pkgembed.PurposeQuery, Texts: []string{strings.TrimSpace(string(content))}})

	if !errors.Is(err, secrets.ErrCredential) {
		t.Fatalf("the query should be refused before the provider: %v", err)
	}
}

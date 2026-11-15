package factory_test

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/test"
)

// Every runner the factory builds refuses a prompt holding a credential before
// the provider sees it.
func TestFactoryRunnerRefusesACredentialBeforeTheProvider(t *testing.T) {
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/slack-bot-token.txt")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	runner, err := factory.New(model.LLMConfig{Provider: "ollama", Model: "llama3", OllamaEndpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	_, err = runner.Run(t.Context(), llm.Request{Purpose: llm.PurposeSummarize, UserPrompt: "Context:\n" + strings.TrimSpace(string(content))})

	if !errors.Is(err, secrets.ErrCredential) {
		t.Fatalf("the prompt should be refused as holding a credential: %v", err)
	}
	if requests.Load() != 0 {
		t.Error("the provider must receive nothing")
	}
}

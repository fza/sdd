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

// Every runner the factory builds refuses a draft holding a credential before
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
	runner, err := factory.New(model.LLMConfig{
		Provider: "openai", Model: "gpt-test", Endpoint: server.URL,
		APIKeys: map[string]string{"openai": "sk-test-not-a-key"},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := "Context:\n" + strings.TrimSpace(string(content))
	ctx := secrets.WithNewParts(t.Context(), secrets.Part{Where: "body", Text: body})

	_, err = runner.Run(ctx, llm.Request{Purpose: llm.PurposeSummarize, UserPrompt: body})

	if !errors.Is(err, secrets.ErrCredential) {
		t.Fatalf("the draft should be refused as holding a credential: %v", err)
	}
	if requests.Load() != 0 {
		t.Error("the provider must receive nothing")
	}
}

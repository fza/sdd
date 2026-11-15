package secrets_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/sdd/test"
)

func credentialSample(t *testing.T) string {
	t.Helper()
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/github-pat.txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(content))
}

func TestGuardRunner(t *testing.T) {
	credential := credentialSample(t)
	cases := []struct {
		name    string
		request llm.Request
		refused bool
	}{
		{name: "credential in the user prompt", request: llm.Request{UserPrompt: "Context:\n" + credential}, refused: true},
		{name: "credential in the system prompt", request: llm.Request{SystemPrompt: credential, UserPrompt: "Hi."}, refused: true},
		{name: "clean prompt", request: llm.Request{SystemPrompt: "Judge the draft.", UserPrompt: "A clean draft."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			guarded := secrets.GuardRunner(llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) {
				calls++
				return llm.Result{Text: "ok"}, nil
			}))

			_, err := guarded.Run(t.Context(), tc.request)

			if tc.refused {
				if !errors.Is(err, secrets.ErrCredential) || !strings.Contains(err.Error(), "refusing to send to the model provider") {
					t.Errorf("the request should be refused before the provider: %v", err)
				}
				if calls != 0 {
					t.Error("a refused request must not reach the provider")
				}
				if strings.Contains(err.Error(), credential) {
					t.Error("the refusal must never repeat the credential")
				}
				return
			}
			if err != nil || calls != 1 {
				t.Errorf("a clean request should pass through: %v, %d calls", err, calls)
			}
		})
	}
}

func TestGuardEmbedder(t *testing.T) {
	credential := credentialSample(t)
	cases := []struct {
		name    string
		request embed.Request
		where   string
	}{
		{name: "query", request: embed.Request{Purpose: embed.PurposeQuery, Texts: []string{credential}}, where: "the query"},
		{name: "document batch", request: embed.Request{Purpose: embed.PurposeDocument, Texts: []string{"clean", credential}}, where: "the text 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			guarded := secrets.GuardEmbedder(embed.EmbedderFunc{Space: "space", Run: func(context.Context, embed.Request) (embed.Result, error) {
				calls++
				return embed.Result{}, nil
			}})

			_, err := guarded.Embed(t.Context(), tc.request)

			if !errors.Is(err, secrets.ErrCredential) || !strings.Contains(err.Error(), tc.where) {
				t.Errorf("the request should be refused naming the text: %v", err)
			}
			if calls != 0 {
				t.Error("a refused request must not reach the provider")
			}
			if guarded.Fingerprint() != "space" {
				t.Error("the guard should keep the embedder's vector space")
			}
		})
	}
}

func TestSearchPhrase(t *testing.T) {
	literal, notice, err := secrets.SearchPhrase(credentialSample(t))
	if err != nil {
		t.Fatal(err)
	}
	if !literal || !strings.Contains(notice, "searched for it literally") {
		t.Errorf("a credential-shaped phrase should be searched literally with a notice, got %v %q", literal, notice)
	}

	literal, notice, err = secrets.SearchPhrase("how the importer handles recipes")
	if err != nil || literal || notice != "" {
		t.Errorf("a clean phrase should pass unchanged, got %v %q %v", literal, notice, err)
	}
}

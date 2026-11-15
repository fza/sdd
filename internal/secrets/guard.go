package secrets

import (
	"context"
	"fmt"
	"strconv"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
)

// GuardRunner refuses every LLM request whose prompt holds a credential, so
// nothing a draft, an entry or an attachment carries reaches the provider.
func GuardRunner(runner llm.Runner) llm.Runner {
	return llm.RunnerFunc(func(ctx context.Context, req llm.Request) (llm.Result, error) {
		for _, prompt := range []string{req.SystemPrompt, req.UserPrompt} {
			findings, err := Scan(prompt)
			if err != nil {
				return llm.Result{}, err
			}
			if len(findings) > 0 {
				return llm.Result{}, &providerRefusal{provider: "model", description: Describe("prompt", findings)}
			}
		}
		return runner.Run(ctx, req)
	})
}

// GuardEmbedder refuses every embedding request holding a credential. Callers
// keep such text away from it in the first place (see SearchPhrase and the
// indexer's chunk filter); this is the boundary that holds when one does not.
func GuardEmbedder(embedder embed.Embedder) embed.Embedder {
	return guardedEmbedder{inner: embedder}
}

type guardedEmbedder struct{ inner embed.Embedder }

func (g guardedEmbedder) Embed(ctx context.Context, req embed.Request) (embed.Result, error) {
	for i, text := range req.Texts {
		findings, err := Scan(text)
		if err != nil {
			return embed.Result{}, err
		}
		if len(findings) == 0 {
			continue
		}
		where := "query"
		if req.Purpose != embed.PurposeQuery {
			where = "text " + strconv.Itoa(i+1)
		}
		return embed.Result{}, &providerRefusal{provider: "embedding", description: Describe(where, findings)}
	}
	return g.inner.Embed(ctx, req)
}

func (g guardedEmbedder) Fingerprint() string { return g.inner.Fingerprint() }

type providerRefusal struct{ provider, description string }

func (e *providerRefusal) Error() string {
	return fmt.Sprintf("refusing to send to the %s provider: %s", e.provider, e.description)
}

func (e *providerRefusal) Unwrap() error { return ErrCredential }

// SearchPhrase keeps a credential-shaped search phrase off the embedding
// provider: such a phrase becomes a literal term and the notice says so. A
// clean phrase returns unchanged with no notice.
func SearchPhrase(phrase string) (literal bool, notice string, err error) {
	findings, err := Scan(phrase)
	if err != nil || len(findings) == 0 {
		return false, "", err
	}
	return true, Describe("query phrase", findings) + "; searched for it literally instead of sending it to the embedding provider", nil
}

// IndexableChunk reports whether a chunk may go to the embedding provider; a
// chunk holding a credential stays out of the vector index, while text search
// still finds its entry.
func IndexableChunk(text string) (bool, string, error) {
	findings, err := Scan(text)
	if err != nil {
		return false, "", err
	}
	if len(findings) == 0 {
		return true, "", nil
	}
	return false, Describe("chunk", findings), nil
}

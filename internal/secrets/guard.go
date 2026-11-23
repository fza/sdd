package secrets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
)

type newPartsKey struct{}

type newParts struct {
	parts []Part
	none  bool
}

// WithNewParts marks the parts of the requests made under ctx that are new to
// the graph, such as a draft's body. GuardRunner scans only these.
func WithNewParts(ctx context.Context, parts ...Part) context.Context {
	marked, _ := ctx.Value(newPartsKey{}).(newParts)
	marked.parts = append(slices.Clip(marked.parts), parts...)
	return context.WithValue(ctx, newPartsKey{}, marked)
}

// WithNoNewParts marks the requests made under ctx as carrying stored text
// alone, such as a summary of an entry already in the graph.
func WithNoNewParts(ctx context.Context) context.Context {
	marked, _ := ctx.Value(newPartsKey{}).(newParts)
	marked.none = true
	return context.WithValue(ctx, newPartsKey{}, marked)
}

// ErrUnmarked refuses a request whose caller never said which text is new; an
// empty WithNewParts counts as unmarked, since a computed list may come out
// empty by mistake.
var ErrUnmarked = errors.New("refusing to send to the model provider: the request marks no new parts (secrets.WithNewParts, or secrets.WithNoNewParts for stored text alone)")

// GuardRunner refuses every LLM request whose new parts (see WithNewParts)
// hold a credential, and every request that marks none at all. Stored entries
// a prompt carries as context pass: a hit in one would otherwise block every
// request pulling it in, and sdd lint reports it.
func GuardRunner(runner llm.Runner) llm.Runner {
	return llm.RunnerFunc(func(ctx context.Context, req llm.Request) (llm.Result, error) {
		marked, _ := ctx.Value(newPartsKey{}).(newParts)
		if len(marked.parts) == 0 && !marked.none {
			return llm.Result{}, ErrUnmarked
		}
		part, findings, err := scanParts(marked.parts)
		if err != nil {
			return llm.Result{}, err
		}
		if len(findings) > 0 {
			return llm.Result{}, &providerRefusal{provider: "model", description: Describe(part.Where, findings)}
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

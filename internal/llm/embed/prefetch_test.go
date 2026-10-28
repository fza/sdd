package embed_test

import (
	"context"
	"errors"
	"testing"

	"github.com/networkteam/sdd/internal/llm/embed"
	pkgembed "github.com/networkteam/sdd/pkg/llm/embed"
)

func TestPrefetchAnswersTheStartedRequestOnce(t *testing.T) {
	queryCalls := make(chan struct{}, 2)
	release := make(chan struct{})
	next := pkgembed.EmbedderFunc{Space: "space", Run: func(_ context.Context, req pkgembed.Request) (pkgembed.Result, error) {
		if req.Texts[0] == "query" {
			queryCalls <- struct{}{}
			<-release
		}
		return pkgembed.Result{Vectors: [][]float32{{float32(len(req.Texts[0]))}}}, nil
	}}
	query := pkgembed.Request{Purpose: pkgembed.PurposeQuery, Texts: []string{"query"}}

	embedder := embed.Prefetch(t.Context(), next, query)
	<-queryCalls
	other, err := embedder.Embed(t.Context(), pkgembed.Request{Purpose: pkgembed.PurposeQuery, Texts: []string{"other text"}})
	if err != nil || other.Vectors[0][0] != 10 {
		t.Fatalf("another request should go to the wrapped embedder, got %+v (%v)", other, err)
	}
	close(release)
	result, err := embedder.Embed(t.Context(), query)
	if err != nil || result.Vectors[0][0] != 5 {
		t.Fatalf("the started request should be answered from its result, got %+v (%v)", result, err)
	}
	if len(queryCalls) != 0 {
		t.Error("the started request should reach the provider once")
	}
	if embedder.Fingerprint() != "space" {
		t.Error("the fingerprint should be the wrapped embedder's")
	}
}

func TestPrefetchReportsTheStartedRequestsError(t *testing.T) {
	failure := errors.New("provider down")
	next := pkgembed.EmbedderFunc{Run: func(context.Context, pkgembed.Request) (pkgembed.Result, error) {
		return pkgembed.Result{}, failure
	}}
	query := pkgembed.Request{Purpose: pkgembed.PurposeQuery, Texts: []string{"query"}}

	if _, err := embed.Prefetch(t.Context(), next, query).Embed(t.Context(), query); !errors.Is(err, failure) {
		t.Errorf("the provider's error should reach the caller, got %v", err)
	}
}

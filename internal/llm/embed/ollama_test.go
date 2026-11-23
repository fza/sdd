package embed_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	localembed "github.com/networkteam/sdd/internal/llm/embed"
	"github.com/networkteam/sdd/internal/model"
)

// Every request asks Ollama to keep the model loaded for embedding.keep_alive,
// 30 minutes by default, so a search after a pause meets no cold load.
func TestOllamaEmbedder_KeepAlive(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, configured, sent string
	}{{"default", "", "30m"}, {"configured", "-1s", "-1s"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sent string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					KeepAlive string `json:"keep_alive"`
				}
				err := json.NewDecoder(r.Body).Decode(&body)
				if err != nil {
					t.Error(err)
				}
				sent = body.KeepAlive
				_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0}}})
			}))
			defer srv.Close()
			emb, err := localembed.New(model.EmbeddingConfig{Provider: "ollama", Model: "nomic-embed-text", OllamaEndpoint: srv.URL, KeepAlive: tc.configured})
			if err != nil {
				t.Fatal(err)
			}
			_, err = emb.Embed(context.Background(), documents("a"))
			if err != nil {
				t.Fatal(err)
			}
			if sent != tc.sent {
				t.Fatalf("keep_alive sent %q: the configured value, else the default, should be sent", sent)
			}
		})
	}
	t.Run("invalid", func(t *testing.T) {
		t.Parallel()
		_, err := localembed.New(model.EmbeddingConfig{Provider: "ollama", Model: "nomic-embed-text", KeepAlive: "soon"})
		if err == nil || !strings.Contains(err.Error(), "embedding.keep_alive") {
			t.Fatalf("an unparseable keep_alive should fail naming the key, got %v", err)
		}
	})
}

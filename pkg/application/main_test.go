package application_test

import (
	"os"
	"testing"

	"github.com/networkteam/sdd/internal/testsupport"
)

// SDD_LIVE_EMBED_MODEL opts the indexing exercise into a live Ollama; its
// re-executed worker passes through this TestMain too.
func TestMain(m *testing.M) {
	var opts []testsupport.Option
	if os.Getenv("SDD_LIVE_EMBED_MODEL") != "" {
		opts = append(opts, testsupport.AllowOllama())
	}
	testsupport.RunWithoutLiveLLMs(m, opts...)
}

package application_test

import (
	"os"
	"testing"

	"github.com/networkteam/sdd/internal/testsupport"
)

// SDD_LIVE_EMBED_MODEL opts the indexing exercise into a live Ollama.
func TestMain(m *testing.M) {
	if os.Getenv("SDD_LIVE_EMBED_MODEL") != "" {
		os.Exit(m.Run())
	}
	testsupport.RunWithoutLiveLLMs(m)
}

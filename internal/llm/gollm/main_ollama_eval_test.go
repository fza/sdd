//go:build ollama_eval

package gollm_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/testsupport"
)

// The ollama_eval build calls a live Ollama on purpose.
func TestMain(m *testing.M) {
	testsupport.RunWithoutLiveLLMs(m, testsupport.AllowOllama())
}

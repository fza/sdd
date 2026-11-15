//go:build !eval

package llmops_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/testsupport"
)

// Eval builds call live models on purpose and keep their own TestMain.
func TestMain(m *testing.M) {
	testsupport.RunWithoutLiveLLMs(m)
}

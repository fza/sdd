//go:build search_eval

package finders_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/testsupport"
)

// The search_eval build calls a live embedding provider on purpose.
func TestMain(m *testing.M) {
	testsupport.RunWithoutLiveLLMs(m, testsupport.AllowOllama())
}

package claude_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain lets the test binary stand in for claude when started under that
// name: unlike a shell, it reads PWD as the runner set it.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "claude" {
		pwdStandIn()
		return
	}
	os.Exit(m.Run())
}

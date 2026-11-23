package cliapp_test

import (
	"testing"

	"os"

	"github.com/networkteam/sdd/internal/testsupport"
	"github.com/networkteam/sdd/test"
)

func TestMain(m *testing.M) {
	// Started as git's merge driver, the binary merges and exits there.
	if len(os.Args) > 1 && os.Args[1] == "git-merge" {
		test.RunTestsAsSDD(m)
	}
	testsupport.RunWithoutLiveLLMs(m)
}

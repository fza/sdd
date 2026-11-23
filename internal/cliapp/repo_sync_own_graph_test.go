package cliapp_test

import (
	"strings"
	"testing"
)

// sdd repo sync naming the project's own graph repository syncs its graph
// clone and never refuses it as a dependency of itself.
func TestRepoSyncOfTheOwnGraphRepository(t *testing.T) {
	code, _ := graphRepoCode(t)
	out, err := runAppErr(t, code, "repo", "sync", string(graphCloneRepoID))
	if err != nil {
		t.Fatalf("syncing the own graph repository should succeed: %v\n%s", err, out)
	}
	if strings.Contains(out, "refused") || !strings.Contains(out, string(graphCloneRepoID)+": ") {
		t.Errorf("the own graph repository should be reported as synced:\n%s", out)
	}
}

// A project naming its own repo_id while its graph lives in the checkout
// skips it, naming the command that syncs it.
func TestRepoSyncOfTheOwnRepoIDInTheCheckout(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/acme/plain\ngraph_dir: .sdd/graph\n")
	f.write(t, ".sdd/graph/.keep", "")
	result := f.run(t, "repo", "sync", "example.test/acme/plain")
	if result.err != nil {
		t.Fatalf("naming the own repo_id should not fail: %v\n%s%s", result.err, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "example.test/acme/plain: skipped (this project's own graph; `sdd sync` syncs it)") {
		t.Errorf("the own graph should be skipped with the command that syncs it:\n%s", result.stdout)
	}
}

package local_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	app "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

// A serving checkout gone from disk, a removed dependency clone for one,
// reads as a missing checkout naming the fix, not as a git failure.
func TestGitWorktreeAcquirerBaseBranchOfAMissingCheckout(t *testing.T) {
	acquirer, err := local.NewGitWorktreeAcquirer(local.GitWorktreeAcquirerOptions{
		Project:        "github.com/acme/platform",
		ServerCheckout: filepath.Join(t.TempDir(), "gone"),
		Factory: func(context.Context, string, app.MutationTarget) (app.GraphStore, []app.MutationFinalizer, func() error, error) {
			return nil, nil, nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = acquirer.BaseBranch(t.Context())
	if !errors.Is(err, local.ErrCheckoutMissing) {
		t.Fatalf("a missing checkout should be reported as such, got %v", err)
	}
	if !strings.Contains(err.Error(), "sdd repo sync github.com/acme/platform") {
		t.Errorf("the error should name the fix, got %v", err)
	}
}

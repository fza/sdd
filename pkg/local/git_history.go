package local

import (
	"context"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
	app "github.com/networkteam/sdd/pkg/application"
)

// GitHistory reads the git history of the checkout at Dir for read_git and
// the deep verifier's file sources at a revision: the production
// app.GitReader.
type GitHistory struct {
	Dir string
}

// ReadGit runs one read-only git read in the checkout.
func (h GitHistory) ReadGit(ctx context.Context, request app.GitReadRequest) (string, error) {
	return git.CLI{Dir: h.Dir}.ReadGit(ctx, model.GitReadRequest{
		Op: model.GitReadOp(request.Op), Rev: request.Rev, From: request.From, To: request.To, Path: request.Path, Limit: request.Limit,
	})
}

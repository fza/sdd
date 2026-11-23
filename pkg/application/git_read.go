package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
)

// ErrGitReadRefused marks a git read refused for its input: an unknown
// operation, a revision or path of a refused form, or one the repository does
// not hold. A GitReader returns it wrapped; ReadGit answers it as an invalid
// argument.
var ErrGitReadRefused = model.ErrGitReadRefused

// GitReader reads the project checkout's git history: the content of a file
// at a revision, a log, one commit, or a diff between two revisions, as
// 20261123-114150-d-tac-d8f defines them. It never writes.
type GitReader interface {
	ReadGit(ctx context.Context, request GitReadRequest) (string, error)
}

// GitReadRequest is one read of the project's git history. Op is file, log,
// commit or diff; Rev, From, To and Path are what that operation names, as
// model.GitReadRequest documents.
type GitReadRequest struct {
	Op       string
	Rev      string
	From, To string
	Path     string
	Limit    int
}

// gitReadModel is the request in the model's terms.
func gitReadModel(r GitReadRequest) model.GitReadRequest {
	return model.GitReadRequest{Op: model.GitReadOp(r.Op), Rev: r.Rev, From: r.From, To: r.To, Path: r.Path, Limit: r.Limit}
}

// GitReadResult is the text a git read returned.
type GitReadResult struct {
	Project ProjectRef
	Text    string
}

// ReadGit reads the project checkout's git history. A project whose runtime
// has no checkout answers that the read is not available.
func (a *Application) ReadGit(ctx context.Context, identity RequestIdentity, project ProjectID, request GitReadRequest) (GitReadResult, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return GitReadResult{}, err
	}
	err = gitReadModel(request).Validate()
	if err != nil {
		return GitReadResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: err.Error(), Cause: err}
	}
	reader := runtime.options.Git
	if reader == nil {
		return GitReadResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf("read_git is not available: project %s has no checkout this server reads", runtime.options.Project.ID)}
	}
	text, err := reader.ReadGit(ctx, request)
	if errors.Is(err, ErrGitReadRefused) {
		return GitReadResult{}, &ApplicationError{Code: ErrorInvalidArgument, Message: err.Error(), Cause: err}
	}
	if err != nil {
		return GitReadResult{}, fmt.Errorf("reading git history: %w", err)
	}
	return GitReadResult{Project: runtime.options.Project, Text: text}, nil
}

// gitFileAt is the file-at-revision lookup of the deep verifier's sources,
// read through reader; nil when the runtime has no reader.
func gitFileAt(ctx context.Context, reader GitReader) func(rev, path string) (string, bool, error) {
	if reader == nil {
		return nil
	}
	return func(rev, path string) (string, bool, error) {
		text, err := reader.ReadGit(ctx, GitReadRequest{Op: string(model.GitReadFile), Rev: rev, Path: path})
		if errors.Is(err, ErrGitReadRefused) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return text, true, nil
	}
}

package local

import (
	"context"
	"time"

	"github.com/networkteam/sdd/internal/git"
	app "github.com/networkteam/sdd/pkg/application"
)

// GitHandEdits finds hand edits of committed entries in the Git history of
// a graph directory (20261109-004506-d-cpt-udz, D5). A graph outside Git, or
// in a repository without commits, has no history and so no hand edits.
type GitHandEdits struct {
	// GraphDir is the graph directory inside a Git checkout.
	GraphDir string
}

func (g GitHandEdits) HandEdits(ctx context.Context, since time.Time) ([]app.HandEdit, error) {
	repo := git.CLI{Dir: g.GraphDir}
	if !repo.InWorkTree(ctx, g.GraphDir) {
		return nil, nil
	}
	head, err := repo.HeadRevision(ctx, g.GraphDir)
	if err != nil || head == "" {
		return nil, err
	}
	edits, err := repo.HandEdits(ctx, since)
	if err != nil {
		return nil, err
	}
	out := make([]app.HandEdit, len(edits))
	for i, edit := range edits {
		out[i] = app.HandEdit{Commit: edit.Commit, Entry: edit.Entry, Time: edit.Time}
	}
	return out, nil
}

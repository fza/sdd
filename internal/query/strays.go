package query

import "github.com/networkteam/sdd/internal/model"

// GraphStraysQuery asks for the entries left in the former locations of the
// graph that the graph at Current does not hold.
type GraphStraysQuery struct {
	// WorkRoot is the repository sdd runs in, the one git-dir and relative
	// git-path locations are resolved against.
	WorkRoot string
	// Repos are the repositories whose history carries the move records:
	// WorkRoot and the one holding the graph.
	Repos []string
	// Current is where the graph lives now, at the folder GraphDir.
	Current  model.LocationRef
	GraphDir string
	// CloneDir is where the sdd-owned clone of a repository lives on this
	// machine.
	CloneDir func(model.RepoID) (string, error)
}

// GraphStraysResult lists the strays and the location they belong in.
type GraphStraysResult struct {
	Current model.LocationRef
	Strays  []model.Stray
	// Records are the move records read, newest first per repository.
	Records []model.MoveRecord
}

// Refusal is the refusal commands meet while strays exist, nil without.
func (r *GraphStraysResult) Refusal() error {
	return model.StrayRefusal(r.Strays, r.Current)
}

package command

import "github.com/networkteam/sdd/internal/model"

// EnsureGraphCloneCmd captures intent to make the sdd-owned clone of a graph
// repository ready for an operation (20261106-202055-d-tac-eyl): cloned when
// missing, kept to what only sdd made otherwise.
type EnsureGraphCloneCmd struct {
	RepoID model.RepoID
	// CodeOrigin is the origin URL of the code repository naming the graph;
	// it picks the protocol a new clone tries first.
	CodeOrigin string
	OnCloned   func(dir string)
}

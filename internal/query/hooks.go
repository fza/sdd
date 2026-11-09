package query

import "github.com/networkteam/sdd/internal/model"

// HooksQuery asks for every declared hook with its confirmation state
// (20261109-213250-d-tac-bve): the user-global config's, and, inside a
// project, those of its repositories. Root and WorkRoot are the project's
// roots, equal outside a project_dir layout and empty outside a project;
// GraphRepo is set for a project reached through graph_repo. LocalConfig
// replaces the project's machine-local layer file; StateRoot holds the
// confirmation store.
type HooksQuery struct {
	GlobalConfig string
	Root         string
	WorkRoot     string
	GraphRepo    model.RepoID
	LocalConfig  string
	StateRoot    string
}

// HooksResult lists the hooks in layer order: user-global, work repository,
// project repository, committed before local.
type HooksResult struct {
	Hooks []model.Hook
}

// HookReviewQuery asks what a person sees before confirming Hook.
type HookReviewQuery struct {
	Hook model.Hook
}

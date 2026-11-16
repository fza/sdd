package query

import (
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/model"
)

// HooksQuery asks for every hook Sources declares with whether it may run
// on this machine; StateRoot holds the allow record.
type HooksQuery struct {
	Sources   hooks.Sources
	StateRoot string
}

// HooksResult lists the hooks in layer order: user-global, work repository,
// project repository, committed before local.
type HooksResult struct {
	Hooks []model.Hook
}

// HookReviewsQuery asks what a person sees before allowing each repository
// whose hooks Sources declares and this machine has not allowed as they are
// now.
type HookReviewsQuery struct {
	Sources   hooks.Sources
	StateRoot string
}

package command

import (
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/model"
)

// AllowHooksCmd records the hooks of each reviewed repository Sources
// declares as allowed on this machine, in the allow record under StateRoot.
type AllowHooksCmd struct {
	Sources   hooks.Sources
	StateRoot string
	Reviews   []model.HookReview
}

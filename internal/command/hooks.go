package command

import "github.com/networkteam/sdd/internal/model"

// ConfirmHookCmd records Hook's hash as confirmed by the person on this
// machine in the confirmation store under StateRoot
// (20261109-213250-d-tac-bve).
type ConfirmHookCmd struct {
	StateRoot string
	Hook      model.Hook
}

package command

import "github.com/networkteam/sdd/internal/model"

// ConfirmHookCmd records Hook's hash as confirmed by the person on this
// machine in the confirmation store under StateRoot.
type ConfirmHookCmd struct {
	StateRoot string
	Hook      model.Hook
}

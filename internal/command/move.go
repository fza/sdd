package command

import "github.com/networkteam/sdd/internal/model"

// MoveGraphCmd moves the graph of the repository at WorkRoot to Target with
// its history, or, when the graph lives at Target already, carries the
// entries left in its former locations over to it.
type MoveGraphCmd struct {
	// WorkRoot is the repository sdd runs in; git-dir locations and a
	// relative git-path name places in or beside it.
	WorkRoot string
	// Target is where the graph goes. A git-path or git-repo target without
	// the dir option takes model.GraphOnlyGraphDir, a git-repo target
	// without a branch the one its clone checks out.
	Target model.LocationRef
	// LocalConfigOverride is the --local-config file, which stays where it is.
	LocalConfigOverride string
	// BinaryVersion is the running sdd's version, the ceiling of the
	// minimum_version the move sets.
	BinaryVersion string

	// OnMoved fires once the graph moved, with the record its commits carry.
	OnMoved func(record model.MoveRecord)
	// OnStraysCarried fires once the entries left in former locations were
	// carried over.
	OnStraysCarried func(record model.MoveRecord, strays []model.Stray)
}

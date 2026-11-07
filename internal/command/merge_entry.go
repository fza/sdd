package command

// MergeEntryCmd captures intent to merge one graph entry file as git's merge
// driver (20261030-085158-d-tac-vyt): the three versions git hands over, with
// the result written into OursPath.
type MergeEntryCmd struct {
	BasePath   string
	OursPath   string
	TheirsPath string
	// MarkerSize is the conflict marker length git asks for; zero keeps git
	// merge-file's default.
	MarkerSize int
}

// MergeConfigCmd captures intent to merge `.sdd/config.yaml` as the sdd-config
// merge driver of the sdd-owned graph clone (20261106-202055-d-tac-eyl), with
// the result written into OursPath and a notice per key both sides changed.
type MergeConfigCmd struct {
	BasePath   string
	OursPath   string
	TheirsPath string
	OnNotice   func(notice string)
}

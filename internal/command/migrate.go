package command

// MigrateSchemaCmd captures intent to raise .sdd/meta.json's
// graph_schema_version to the version this binary writes. Approval is the
// caller's: the handler stamps unconditionally.
type MigrateSchemaCmd struct {
	// SDDDir is the absolute path to the .sdd/ directory.
	SDDDir string

	// SchemaVersion is the version to stamp, model.CurrentGraphSchemaVersion
	// at the call site.
	SchemaVersion int

	// OnStamped fires after meta.json was rewritten, carrying the previous
	// and the stamped version.
	OnStamped func(from, to int)
}

// CommitStrandedTitlesCmd captures intent to commit entry files that hold a
// title on disk but not at HEAD.
type CommitStrandedTitlesCmd struct {
	// OnCommitted fires after the stranded files were committed, carrying
	// their count.
	OnCommitted func(count int)
}

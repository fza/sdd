package query

import "github.com/networkteam/sdd/internal/model"

// InfoQuery captures intent to read session framing — participant
// identity, search capability, configured graph language and graph schema.
// SDDDir locates the graph's meta.json; empty leaves the schema unreported.
type InfoQuery struct {
	SDDDir string
}

// InfoResult is the structured snapshot of session framing.
//
// LocalParticipant surfaces the canonical participant name from
// .sdd/config.local.yaml so agents reading the header see ground truth
// without inferring from entries (which may carry drift). Empty means
// "not configured".
//
// Language surfaces the configured graph language (locale code) from
// .sdd/config.yaml so the /sdd skill knows whether to load a translation
// vocabulary and author entries in the configured language. Empty means
// English (default) — the line is suppressed at render time.
//
// Search renders as `text` when only text mode is available and
// `vector,text` when an embedding provider is configured.
//
// GraphSchema is the schema the graph's meta.json records, zero when unknown;
// BinarySchema is the schema this binary writes by default. Writes happen at
// GraphSchema when it is older.
type InfoResult struct {
	LocalParticipant string
	Language         string
	Search           string
	GraphSchema      int
	BinarySchema     int
	// Capabilities describes the schema writes use (model.WriteSchemaVersion);
	// nil when that schema is unknown to this binary.
	Capabilities *model.SchemaCapabilities
	// Contribution is the machine-local contribution ref, empty outside
	// contribution mode.
	Contribution string
}

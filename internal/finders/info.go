package finders

import (
	"context"
	"errors"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// Info gathers session framing for the `sdd info` command — the session
// header surface that skill session-start injections read. Config and
// meta.json inspection — no graph access required.
func (f *Finder) Info(ctx context.Context, q query.InfoQuery) (*query.InfoResult, error) {
	participant, err := f.localParticipant()
	if err != nil {
		return nil, err
	}
	language, err := f.language()
	if err != nil {
		return nil, err
	}
	result := &query.InfoResult{
		LocalParticipant: participant,
		Language:         language,
		Search:           f.searchCapability(),
		BinarySchema:     model.CurrentGraphSchemaVersion,
	}
	if f.cfg != nil {
		result.Contribution = f.cfg.Contribution
	}
	var meta *model.SchemaMeta
	if q.SDDDir != "" {
		status, err := f.SchemaStatus(ctx, query.SchemaStatusQuery{SDDDir: q.SDDDir, BinarySchemaVersion: model.CurrentGraphSchemaVersion})
		if err != nil {
			return nil, err
		}
		if status.MetaExists {
			meta = status.Meta
			result.GraphSchema = meta.GraphSchemaVersion
		}
	}
	capabilities, err := model.CapabilitiesFor(model.WriteSchemaVersion(meta))
	// A graph newer than the binary has no capabilities this binary knows;
	// info still reports it, and the write gate refuses the write.
	if errors.Is(err, model.ErrUnknownSchemaVersion) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result.Capabilities = &capabilities
	return result, nil
}

// searchCapability reports whether vector search is configured. Returns
// "vector,text" when an embedding provider is set, "text" otherwise.
// Pure config inspection — actual index health (drift, missing rows) is
// reported by `sdd lint`.
func (f *Finder) searchCapability() string {
	if f.cfg != nil && f.cfg.Embedding.Provider != "" {
		return "vector,text"
	}
	return "text"
}

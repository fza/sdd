package handlers

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// writeSchema is the graph schema a write produces and what it supports. A
// graph older than the binary keeps its own shape until `sdd migrate`.
type writeSchema struct {
	version      int
	capabilities model.SchemaCapabilities
}

// resolveWriteSchema reads the graph's schema on every write, so a migration
// between two writes of one process is honoured. A handler without an .sdd
// directory, or a graph without meta.json, writes at the binary's schema.
func (h *Handler) resolveWriteSchema(ctx context.Context) (writeSchema, error) {
	var meta *model.SchemaMeta
	if h.sddDir != "" {
		status, err := h.reader.SchemaStatus(ctx, query.SchemaStatusQuery{SDDDir: h.sddDir, BinarySchemaVersion: model.CurrentGraphSchemaVersion})
		if err != nil {
			return writeSchema{}, fmt.Errorf("reading the graph schema: %w", err)
		}
		meta = status.Meta
	}
	version := model.WriteSchemaVersion(meta)
	capabilities, err := model.CapabilitiesFor(version)
	if err != nil {
		return writeSchema{}, err
	}
	return writeSchema{version: version, capabilities: capabilities}, nil
}

// refuseTitle reports an explicit title a graph's schema cannot hold.
func (s writeSchema) refuseTitle() error {
	return fmt.Errorf("the graph is at schema %d, which has no entry titles; leave out the title or run `sdd migrate` to upgrade the graph", s.version)
}

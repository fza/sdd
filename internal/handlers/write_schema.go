package handlers

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// checkWriteSchema refuses a write to a graph whose schema this binary does
// not know. It reads the graph's schema on every write, so a migration
// between two writes of one process is honoured. A handler without an .sdd
// directory, or a graph without meta.json, writes at the binary's schema.
func (h *Handler) checkWriteSchema(ctx context.Context) error {
	var meta *model.SchemaMeta
	if h.sddDir != "" {
		status, err := h.reader.SchemaStatus(ctx, query.SchemaStatusQuery{SDDDir: h.sddDir, BinarySchemaVersion: model.CurrentGraphSchemaVersion})
		if err != nil {
			return fmt.Errorf("reading the graph schema: %w", err)
		}
		meta = status.Meta
	}
	_, err := model.CapabilitiesFor(model.WriteSchemaVersion(meta))
	return err
}

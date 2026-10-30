package application

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
)

// GraphSchemaReader is an optional GraphStore capability: the schema version
// the graph records. A graph older than the binary is written at its own
// schema until an explicit migration; a store without the capability is at
// the binary's schema.
type GraphSchemaReader interface {
	GraphSchemaVersion(context.Context) (int, error)
}

func checkGraphSchema(ctx context.Context, graph GraphStore) error {
	version := model.CurrentGraphSchemaVersion
	if reader, ok := graph.(GraphSchemaReader); ok {
		recorded, err := reader.GraphSchemaVersion(ctx)
		if err != nil {
			return fmt.Errorf("reading the graph schema: %w", err)
		}
		version = recorded
	}
	_, err := model.CapabilitiesFor(version)
	return err
}

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

type graphSchema struct {
	version      int
	capabilities model.SchemaCapabilities
}

func graphSchemaOf(ctx context.Context, graph GraphStore) (graphSchema, error) {
	version := model.CurrentGraphSchemaVersion
	if reader, ok := graph.(GraphSchemaReader); ok {
		recorded, err := reader.GraphSchemaVersion(ctx)
		if err != nil {
			return graphSchema{}, fmt.Errorf("reading the graph schema: %w", err)
		}
		version = recorded
	}
	capabilities, err := model.CapabilitiesFor(version)
	if err != nil {
		return graphSchema{}, err
	}
	return graphSchema{version: version, capabilities: capabilities}, nil
}

func (s graphSchema) refuseTitle() error {
	return &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf("the graph is at schema %d, which has no entry titles; leave out the title or run `sdd migrate` to upgrade the graph", s.version)}
}

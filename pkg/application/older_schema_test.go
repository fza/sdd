package application_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

func TestInfoReportsTheGraphSchema(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema int
		want   int
	}{
		{name: "graph at schema 1", schema: 1, want: 1},
		{name: "graph without meta.json", want: model.CurrentGraphSchemaVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newWriteFixture(t, writeFixtureOptions{GraphSchema: tc.schema})
			info, err := f.app.Info(t.Context(), f.identity, "example", sdd.InfoRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if info.GraphSchema != tc.want || info.BinarySchema != model.CurrentGraphSchemaVersion {
				t.Errorf("info should report the graph's and the binary's schema, got graph %d binary %d", info.GraphSchema, info.BinarySchema)
			}
		})
	}
}

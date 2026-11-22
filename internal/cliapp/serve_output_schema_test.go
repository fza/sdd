package cliapp_test

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestServeDeclaresOutputSchemasOnlyWithTheFlag(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		want  bool
	}{
		{name: "default"},
		{name: "output-schema flag", flags: []string{"--output-schema"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppFixture(t)
			fixture.serveArgs(t, tc.flags, func(client *mcp.ClientSession) {
				res, err := client.ListTools(t.Context(), &mcp.ListToolsParams{})
				if err != nil {
					t.Fatal(err)
				}
				if len(res.Tools) == 0 {
					t.Fatal("tools/list returned no tools")
				}
				for _, tool := range res.Tools {
					if got := tool.OutputSchema != nil; got != tc.want {
						t.Errorf("tool %s: outputSchema declared = %v, want %v", tool.Name, got, tc.want)
					}
				}
			})
		})
	}
}

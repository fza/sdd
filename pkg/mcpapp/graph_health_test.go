package mcpapp_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

var healthHeadline = regexp.MustCompile(`Graph health: (\d+) warning\(s\)`)

// healthNotice returns the warning count and the issue lines of a framing's
// graph-health notice.
func healthNotice(t *testing.T, framing string) (int, []string) {
	t.Helper()
	_, notice, ok := strings.Cut(framing, "Graph health: ")
	if !ok {
		t.Fatalf("framing carries no graph-health notice:\n%s", framing)
	}
	lines := strings.Split("Graph health: "+notice, "\n")
	m := healthHeadline.FindStringSubmatch(lines[0])
	if m == nil {
		t.Fatalf("graph-health headline unreadable: %q", lines[0])
	}
	count, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatal(err)
	}
	var issues []string
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		issues = append(issues, line)
	}
	return count, issues
}

// TestGraphHealthStableAcrossServes pins that every serve of one session
// reports the graph's own warnings: the count does not grow from serve to
// serve and no warning line repeats, with or without a dependency graph
// assembled into the serve.
func TestGraphHealthStableAcrossServes(t *testing.T) {
	// Each of writeFixtureGraph's two entries names participant "Tester", whom
	// no actor covers.
	const fixtureWarnings = 2
	tests := []struct {
		name string
		open func(t *testing.T) (*mcp.ClientSession, map[string]any)
	}{
		{
			name: "single project",
			open: func(t *testing.T) (*mcp.ClientSession, map[string]any) {
				return connect(t, newTestServer(t, nil, "", "").srv), map[string]any{}
			},
		},
		{
			name: "project with dependency",
			open: func(t *testing.T) (*mcp.ClientSession, map[string]any) {
				return connect(t, newServerOver(t, newDependentAccess(t, "alpha", "beta"))), map[string]any{"project": "alpha"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs, args := tt.open(t)
			var door mcpserver.ServeResult
			call(t, cs, "start_session", args, &door)
			framings := []string{door.Framing}
			for range 3 {
				var resumed mcpserver.ServeResult
				call(t, cs, "resume_session", map[string]any{"session": door.Session}, &resumed)
				framings = append(framings, resumed.Framing)
			}
			for i, framing := range framings {
				got, lines := healthNotice(t, framing)
				if got != fixtureWarnings {
					t.Errorf("serve %d reports %d warning(s), want %d", i, got, fixtureWarnings)
				}
				seen := map[string]bool{}
				for _, line := range lines {
					if seen[line] {
						t.Errorf("serve %d repeats warning line %q", i, line)
					}
					seen[line] = true
				}
				if len(lines) != fixtureWarnings {
					t.Errorf("serve %d lists %d warning line(s), want %d:\n%s", i, len(lines), fixtureWarnings, framing)
				}
			}
		})
	}
}

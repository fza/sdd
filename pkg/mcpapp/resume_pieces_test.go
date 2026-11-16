package mcpapp_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writeChainProcedure writes a procedure whose one step serves a guide lane
// of lines lines and, when chain is set, a lane carrying the fixture gap
// entry in full.
func writeChainProcedure(t *testing.T, graphDir, file, canonical string, lines int, chain bool) {
	t.Helper()
	inject, lane := "", ""
	if chain {
		inject = `
      inject:
          - fn: entryChains
            args:
                id: "` + fixtureGapID + `"
                up: 0
                down: 0`
		lane = "### lane: chain\n\n{{.entryChains}}\n"
	}
	spec := `---
type: decision
kind: procedure
layer: process
canonical: ` + canonical + `
confidence: high
summary: A workflow whose one step serves a guide and maybe an entry.
state:
    body: {type: text, desc: what the step yields}
steps:
    - id: step
      collect: [body]` + inject + `
      transitions:
          - when: hasBody
            to: end(completed)
---

Serves a guide.

## unit: step

### lane: guide

` + strings.Repeat("The guide of "+canonical+" holds a line of its text.\n", lines) + "\n" + lane
	if err := os.WriteFile(filepath.Join(graphDir, file), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A resume points at an entry only under a tag its own text carries: an
// entry served in a move the resume cuts or leaves waiting serves in full
// where a later move holds it, never as a pointer to text the agent never got.
func TestResumePointsOnlyAtEntriesItDelivers(t *testing.T) {
	graphDir := writeFixtureGraph(t)
	writeChainProcedure(t, graphDir, "2026/07/04-120011-d-prc-pl1.md", "plain-big", 200, false)
	writeChainProcedure(t, graphDir, "2026/07/04-120012-d-prc-ch1.md", "chain-big", 120, true)
	writeChainProcedure(t, graphDir, "2026/07/04-120013-d-prc-ch2.md", "chain-small", 2, true)
	env := newTestServer(t, nil, graphDir, "")
	cs := connect(t, env.srv)
	session := openSession(t, cs).Session
	for _, canonical := range []string{"plain-big", "chain-big", "chain-small"} {
		callRaw(t, cs, "start_procedure", map[string]any{"session": session, "canonical": canonical})
	}

	raw := newAgentMemory()
	raw.raw = true
	resumed := callRaw(t, connectAs(t, env.srv, raw), "resume_session", map[string]any{"session": session})
	text := fmt.Sprint(resumed)
	pointer := regexp.MustCompile(regexp.QuoteMeta(fixtureGapID) + ` — [^\n]*?\(held as tag ([0-9a-z]{4})`)
	for _, match := range pointer.FindAllStringSubmatch(text, -1) {
		placed := strings.Count(text, "⟦"+match[1]+"⟧")
		if placed == 0 {
			t.Fatalf("the resume should not point at an entry under a tag it never delivered (%s):\n%s", match[1], text)
		}
	}
	if !strings.Contains(text, fixtureGapID) {
		t.Fatalf("the resume should name the entry the moves carry:\n%s", text)
	}
}

package mcpapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// infoFramedProcedure is a move whose first step carries the info block alone
// and whose second step declares no framing.
const infoFramedProcedure = `---
type: decision
kind: procedure
layer: process
canonical: info-framed
confidence: high
summary: A move whose first step carries the info framing alone.
state:
    body: {type: text, desc: what the step reports}
steps:
    - id: wait
      framing: [info]
      collect: [body]
      transitions:
          - when: hasBody
            to: close
    - id: close
      chooser: agent
      options:
          - {choice: done, to: end(completed)}
---

Frames its first step with info alone.

## unit: wait

Report the body.

## unit: close

Close.
`

func TestStepFramingOverTheTransport(t *testing.T) {
	graphDir := writeFixtureGraph(t)
	path := filepath.Join(graphDir, "2026/07/05-130000-d-prc-ifr.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(infoFramedProcedure), 0o644); err != nil {
		t.Fatal(err)
	}
	env := newTestServer(t, nil, graphDir, "")
	memory := newAgentMemory()
	client := connectAs(t, env.srv, memory)
	door := openSession(t, client)

	// Each call forgets what earlier ones served, so every block it carries
	// serves in full.
	memory.forget = true
	var move mcpserver.ServeResult
	call(t, client, "start_procedure", map[string]any{"session": door.Session, "canonical": "info-framed"}, &move)
	if !strings.Contains(move.Framing, "Local participant:") || strings.Contains(move.Framing, "Recent graph movement") {
		t.Fatalf("a step declaring info framing should carry the info block alone, got %q", move.Framing)
	}
	memory.forget = true
	var closing mcpserver.ServeResult
	call(t, client, "next", map[string]any{"session": door.Session, "instance": move.Instance, "report": map[string]any{"body": "b"}}, &closing)
	if closing.Step != "close" || !strings.Contains(closing.Framing, "Recent graph movement") {
		t.Fatalf("a step declaring no framing should carry the whole framing, at %s with %q", closing.Step, closing.Framing)
	}
}

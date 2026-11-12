package application_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// listedStepProcedure injects a dated listing of every live entry, bounded at
// maxBytes.
func listedStepProcedure(maxBytes int) string {
	return fmt.Sprintf(`---
type: decision
kind: procedure
layer: process
canonical: listed-step
confidence: high
summary: A workflow whose step serves a bounded listing.
state:
    body: {type: text, desc: what the listing showed}
steps:
    - id: look
      inject:
          - {id: listed, fn: viewLayout, args: {layout: 'rank(by(date)):brief:as-list'}, maxBytes: %d}
      collect: [body]
      transitions:
          - when: hasBody
            to: end(completed)
---

Serves a bounded listing.

## unit: look

{{.listed}}
`, maxBytes)
}

var moreLine = regexp.MustCompile("\\(\\+(\\d+) more: call view with layout `([^`]+)`\\)")

// newListedWorkflow opens a session over a graph holding six gaps and the
// listed-step procedure bounded at maxBytes.
func newListedWorkflow(t *testing.T, maxBytes int) (*sdd.Application, *sdd.WorkflowSession, sdd.RequestIdentity) {
	t.Helper()
	graphDir := t.TempDir()
	dir := filepath.Join(graphDir, "2026", "07")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"13-070001-d-prc-lst.md": listedStepProcedure(maxBytes)}
	for i := range 6 {
		files[fmt.Sprintf("14-0%d0000-s-tac-ls%d.md", i, i)] = fmt.Sprintf("---\ntype: signal\nkind: gap\nlayer: tactical\nconfidence: high\nsummary: Listed gap number %d, a line long enough to count.\n---\n\nListed gap number %d.\n", i, i)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	application, _, _, _ := newStampWorkflowApp(t, graphDir, "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "client"})
	if err != nil {
		t.Fatal(err)
	}
	return application, workflow, identity
}

func viewedIDs(t *testing.T, application *sdd.Application, identity sdd.RequestIdentity, layout string) []string {
	t.Helper()
	result, err := application.View(t.Context(), identity, "example", sdd.ViewRequest{Layout: layout})
	if err != nil {
		t.Fatalf("view %q: %v", layout, err)
	}
	var ids []string
	for _, section := range result.ViewSections {
		if section.List != nil {
			for _, item := range section.List.Items {
				ids = append(ids, item.Entry.ID)
			}
		}
	}
	return ids
}

// A listing past its inject's byte bound gives up whole entries and ends
// naming how many and the layout serving them, never a byte-ratio cut.
func TestBoundedListingNamesTheEntriesPastItsBound(t *testing.T) {
	application, workflow, identity := newListedWorkflow(t, 400)
	serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "listed-step"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(serve.Instructions, "cut for size") {
		t.Fatalf("the listing should give up whole entries, not be cut for size:\n%s", serve.Instructions)
	}
	match := moreLine.FindStringSubmatch(serve.Instructions)
	if match == nil {
		t.Fatalf("the listing should name the entries past its bound:\n%s", serve.Instructions)
	}
	all := viewedIDs(t, application, identity, "rank(by(date)):brief:as-list")
	listed := 0
	for _, id := range all {
		if strings.Contains(serve.Instructions, id) {
			listed++
		}
	}
	if more, _ := strconv.Atoi(match[1]); listed == 0 || more != len(all)-listed {
		t.Fatalf("the listing should count the %d entries it gave up, got %s with %d listed", len(all)-listed, match[1], listed)
	}
	if next := viewedIDs(t, application, identity, match[2]); !slices.Equal(next, all[listed:]) {
		t.Errorf("the named layout %q should serve the entries past the bound, got %v", match[2], next)
	}
}

func TestListingWithinItsBoundNamesNoMore(t *testing.T) {
	_, workflow, identity := newListedWorkflow(t, 20000)
	serve, err := workflow.Start(t.Context(), identity, sdd.WorkflowStartRequest{Canonical: "listed-step"})
	if err != nil {
		t.Fatal(err)
	}
	if moreLine.MatchString(serve.Instructions) {
		t.Errorf("a listing within its bound should name no more entries:\n%s", serve.Instructions)
	}
}

package application_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// A staged text file and the pairs editing it lose the anchor tags an agent
// copied in from served text; a binary file and a file read from a path are
// staged as they are.
func TestStagedAttachmentsLoseAnchorTags(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	stage := func(item sdd.StageItem) {
		t.Helper()
		if _, err := workflow.StageAttachments(t.Context(), identity, []sdd.StageItem{item}); err != nil {
			t.Fatal(err)
		}
	}
	read := func(handle string) string {
		t.Helper()
		page, _, err := workflow.ReadStagedAttachment(t.Context(), identity, handle, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return string(page.Content)
	}

	stage(sdd.StageItem{Name: "notes.md", Content: []byte("keep ⟦ab12⟧ this line")})
	if got := read("notes.md"); got != "keep this line" {
		t.Fatalf("a staged text file should lose its anchor tags, got %q", got)
	}
	stage(sdd.StageItem{Name: "notes.md", Patches: []types.PatchPair{{Old: "⟦ab12⟧ this", New: "that ⟦cd34⟧ one"}}})
	if got := read("notes.md"); got != "keep that one line" {
		t.Fatalf("an edited staged file should lose the tags of its pairs, got %q", got)
	}

	binary := append([]byte{0xff, 0xfe}, []byte("⟦ab12⟧ ")...)
	stage(sdd.StageItem{Name: "image.bin", Content: binary})
	if got := read("image.bin"); got != string(binary) {
		t.Fatalf("a binary file should be staged as sent, got %q", got)
	}

	path := filepath.Join(t.TempDir(), "local.md")
	if err := os.WriteFile(path, []byte("on disk ⟦ab12⟧ as written"), 0o644); err != nil {
		t.Fatal(err)
	}
	stage(sdd.StageItem{Name: "local.md", Path: path})
	if got := read("local.md"); got != "on disk ⟦ab12⟧ as written" {
		t.Fatalf("a file read from a path should be staged as it is on disk, got %q", got)
	}
}

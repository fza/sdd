package application_test

import (
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// A staged text file and the pairs editing it lose the anchor tags an agent
// copied in from served text; a binary file is staged as sent.
func TestStagedAttachmentsLoseAnchorTags(t *testing.T) {
	application, _, _, _ := newStampWorkflowApp(t, "", "", time.Now)
	identity := sdd.RequestIdentity{Subject: "christopher"}
	workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	read := func(handle string) string {
		t.Helper()
		page, _, err := workflow.ReadStagedAttachment(t.Context(), identity, handle, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		return string(page.Content)
	}

	if _, err := workflow.StageAttachment(t.Context(), identity, "notes.md", []byte("keep ⟦ab12⟧ this line")); err != nil {
		t.Fatal(err)
	}
	if got := read("notes.md"); got != "keep this line" {
		t.Fatalf("a staged text file should lose its anchor tags, got %q", got)
	}
	_, err = workflow.EditStagedAttachment(t.Context(), identity, "notes.md", []types.PatchPair{{Old: "⟦ab12⟧ this", New: "that ⟦cd34⟧ one"}})
	if err != nil {
		t.Fatalf("a pair quoting a tag should match the stripped file: %v", err)
	}
	if got := read("notes.md"); got != "keep that one line" {
		t.Fatalf("an edited staged file should lose the tags of its pairs, got %q", got)
	}

	binary := append([]byte{0xff, 0xfe}, []byte("⟦ab12⟧ ")...)
	if _, err := workflow.StageAttachment(t.Context(), identity, "image.bin", binary); err != nil {
		t.Fatal(err)
	}
	if got := read("image.bin"); got != string(binary) {
		t.Fatalf("a binary file should be staged as sent, got %q", got)
	}
}

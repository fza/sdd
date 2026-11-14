package mcpapp

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

// The hold-next line keeps a result within its bound by leaving out its least
// likely tags; the room left is reachable only from a result composed near
// the bound, so the cut is tested here.
func TestHoldNextLineLeavesOutTheLastTagsPastItsRoom(t *testing.T) {
	tags := []string{"aaaa", "bbbb", "cccc"}
	whole := holdNextLine(tags, model.MaxResultChars)
	if whole != "aaaa, bbbb, cccc" {
		t.Fatalf("a line with room should name every tag in order, bare, got %q", whole)
	}
	if cut := holdNextLine(tags, model.ServedChars(whole)-1); cut != "aaaa, bbbb" {
		t.Errorf("a line short of room should leave out its last tag, got %q", cut)
	}
	if none := holdNextLine(tags, 3); none != "" {
		t.Errorf("a line with no room for one tag should be left out, got %q", none)
	}
}

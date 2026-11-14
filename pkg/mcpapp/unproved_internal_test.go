package mcpapp

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// The unproved list keeps a result within its bound by leaving out its oldest
// pieces; the room left is reachable only from a result composed near the
// bound, so the cut is tested here.
func TestUnprovedLineLeavesOutTheOldestPiecesPastItsRoom(t *testing.T) {
	list := []sdd.UnprovedPiece{{Name: "lane new", Tag: "aaaa"}, {Name: "lane mid", Tag: "bbbb"}, {Name: "lane old", Tag: "cccc"}}
	whole := unprovedLine(list, model.MaxResultChars)
	if !strings.HasSuffix(whole, "lane new (tag aaaa); lane mid (tag bbbb); lane old (tag cccc)") {
		t.Fatalf("a list with room should name every piece, newest first, got %q", whole)
	}
	cut := unprovedLine(list, model.ServedChars(whole)-1)
	if !strings.HasSuffix(cut, "lane new (tag aaaa); lane mid (tag bbbb)") {
		t.Errorf("a list short of room should leave out its oldest piece, got %q", cut)
	}
	if none := unprovedLine(list, len(unprovedLead)); none != "" {
		t.Errorf("a list with no room for one piece should be left out, got %q", none)
	}
}

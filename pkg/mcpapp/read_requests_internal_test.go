package mcpapp

import "testing"

// The read tools render what they serve with the session's repeats, so the
// requests they send leave the application's own text unrendered. The
// requests are built behind the session a tool call attaches, hence here.
func TestReadToolsRequestDataOnly(t *testing.T) {
	if !(ViewArgs{Layout: "as-list"}).request("", false).DataOnly {
		t.Error("view request renders text the tool does not serve")
	}
	if !(ShowArgs{IDs: []string{"20260101-100000-s-tac-aaa"}}).request(1, 1, "", false).DataOnly {
		t.Error("show request renders text the tool does not serve")
	}
	if !(SearchArgs{Terms: []string{"x"}}).request("", defaultSearchHits, 0, "", false).DataOnly {
		t.Error("search request renders text the tool does not serve")
	}
}

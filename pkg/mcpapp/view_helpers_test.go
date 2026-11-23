package mcpapp

import (
	"strings"
	"testing"
)

func TestEmptyViewMessage(t *testing.T) {
	if got := emptyViewMessage(nil); got != "0 entries matched the layout." {
		t.Errorf("no-participants message = %q", got)
	}
	got := emptyViewMessage([]string{"Christopher", "Jonathan Philipp"})
	if !strings.Contains(got, "0 entries matched") {
		t.Errorf("message missing empty statement: %q", got)
	}
	if !strings.Contains(got, "Known participants: Christopher, Jonathan Philipp") {
		t.Errorf("message missing participant names: %q", got)
	}
}

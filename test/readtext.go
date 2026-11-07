package test

import (
	"testing"

	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/pkg/application"
)

// ShowText / SearchText / ViewText render an application read result as the
// MCP server and the CLI print it, for assertions on the text a reader sees.
func ShowText(t testing.TB, result application.ShowResult) string {
	t.Helper()
	text, err := presenters.ShowText(result.Items)
	if err != nil {
		t.Fatalf("rendering show text: %v", err)
	}
	return text
}

func SearchText(result application.SearchResult) string {
	return presenters.SearchText(result.Hits, result.Notices)
}

func ViewText(result application.ViewResult) string {
	return presenters.ViewText(result.ViewSections)
}

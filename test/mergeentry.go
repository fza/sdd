package test

import (
	"embed"
	"testing"
)

// mergeEntryFixtures are versions of one graph entry for merge driver tests:
// base.md and variants changing its summary, title, body or an unknown
// frontmatter key.
//
//go:embed fixtures/mergeentry
var mergeEntryFixtures embed.FS

// MergeEntryFixture returns the content of the named entry version.
func MergeEntryFixture(t *testing.T, name string) string {
	t.Helper()
	content, err := mergeEntryFixtures.ReadFile("fixtures/mergeentry/" + name)
	if err != nil {
		t.Fatalf("reading merge entry fixture %s: %v", name, err)
	}
	return string(content)
}

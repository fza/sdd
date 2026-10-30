package handlers_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/test"
)

// mergeEntry runs the merge driver over base.md and two fixture versions the
// way git hands them over and returns the merged content and the handler's
// error.
func mergeEntry(t *testing.T, ours, theirs string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{"base": "base.md", "ours": ours, "theirs": theirs}
	for name, fixture := range paths {
		err := os.WriteFile(filepath.Join(dir, name), []byte(test.MergeEntryFixture(t, fixture)), 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	h := handlers.New(handlers.Options{FileMerger: git.CLI{}})
	mergeErr := h.MergeEntry(context.Background(), &command.MergeEntryCmd{
		BasePath:   filepath.Join(dir, "base"),
		OursPath:   filepath.Join(dir, "ours"),
		TheirsPath: filepath.Join(dir, "theirs"),
	})
	merged, err := os.ReadFile(filepath.Join(dir, "ours"))
	if err != nil {
		t.Fatal(err)
	}
	return string(merged), mergeErr
}

func TestMergeEntryResolvesDerivedFields(t *testing.T) {
	tests := []struct {
		name         string
		ours, theirs string
		want         string
	}{
		{name: "both changed the summary: the value sorting first", ours: "summary-b.md", theirs: "summary-a.md", want: "summary-a.md"},
		{name: "only theirs changed the summary", ours: "base.md", theirs: "summary-b.md", want: "summary-b.md"},
		{name: "only ours changed the title", ours: "title-changed.md", theirs: "base.md", want: "title-changed.md"},
		{name: "a summary and a title change combine", ours: "summary-b.md", theirs: "title-changed.md", want: "summary-b-title-changed.md"},
		{name: "an unknown key changed beside a summary survives byte for byte", ours: "summary-b.md", theirs: "key-changed.md", want: "summary-b-key-changed.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := test.MergeEntryFixture(t, tt.want)

			merged, err := mergeEntry(t, tt.ours, tt.theirs)
			if err != nil {
				t.Fatalf("MergeEntry: %v", err)
			}
			if merged != want {
				t.Errorf("merged entry should carry both sides' changes:\n%s\nwant\n%s", merged, want)
			}

			reversed, err := mergeEntry(t, tt.theirs, tt.ours)
			if err != nil {
				t.Fatalf("MergeEntry, reversed: %v", err)
			}
			if reversed != merged {
				t.Errorf("merging from the other side should yield the same file:\n%s\nagainst\n%s", reversed, merged)
			}
		})
	}
}

func TestMergeEntryLeavesConflictMarkersOnOtherLines(t *testing.T) {
	merged, err := mergeEntry(t, "body-ours.md", "body-theirs.md")
	if !errors.Is(err, handlers.ErrEntryConflict) {
		t.Fatalf("a conflicting body line should report a conflict, got %v", err)
	}
	for _, marker := range []string{"<<<<<<< ours", "=======", ">>>>>>> theirs", "Second line, ours.", "Second line, theirs."} {
		if !strings.Contains(merged, marker) {
			t.Errorf("merged entry should hold the conflict with both sides, missing %q:\n%s", marker, merged)
		}
	}
}

// A side whose frontmatter is gone gives no field to resolve against, so the
// whole file merges as text and the removal meets the other side's change.
func TestMergeEntryWithOneSideLackingFrontmatterMergesAsText(t *testing.T) {
	_, err := mergeEntry(t, "body-only.md", "summary-b.md")
	if !errors.Is(err, handlers.ErrEntryConflict) {
		t.Errorf("a removed frontmatter against a changed summary should conflict, got %v", err)
	}
}

type failingMerger struct{}

func (failingMerger) MergeFile(context.Context, string, string, string, int) (bool, error) {
	return false, errors.New("merge-file failed")
}

// The driver command falls back to git's text merge on any failure, which
// needs git's file exactly as git handed it over.
func TestMergeEntryFailureLeavesOursUntouched(t *testing.T) {
	dir := t.TempDir()
	paths := map[string]string{"base": "base.md", "ours": "summary-b.md", "theirs": "summary-a.md"}
	for name, fixture := range paths {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(test.MergeEntryFixture(t, fixture)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := handlers.New(handlers.Options{FileMerger: failingMerger{}})
	err := h.MergeEntry(context.Background(), &command.MergeEntryCmd{
		BasePath:   filepath.Join(dir, "base"),
		OursPath:   filepath.Join(dir, "ours"),
		TheirsPath: filepath.Join(dir, "theirs"),
	})
	if err == nil || errors.Is(err, handlers.ErrEntryConflict) {
		t.Fatalf("a failed merge should report a failure, not a conflict, got %v", err)
	}
	ours, err := os.ReadFile(filepath.Join(dir, "ours"))
	if err != nil {
		t.Fatal(err)
	}
	if string(ours) != test.MergeEntryFixture(t, "summary-b.md") {
		t.Errorf("ours should stay as git handed it over:\n%s", ours)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("no staged result should be left beside ours, found %d files", len(entries))
	}
}

func TestMergeEntryWithoutFrontmatterMergesAsText(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cmd := &command.MergeEntryCmd{
		BasePath:   write("base", "a\nb\nc\n"),
		OursPath:   write("ours", "A\nb\nc\n"),
		TheirsPath: write("theirs", "a\nb\nC\n"),
	}
	h := handlers.New(handlers.Options{FileMerger: git.CLI{}})
	if err := h.MergeEntry(context.Background(), cmd); err != nil {
		t.Fatalf("MergeEntry: %v", err)
	}
	merged, err := os.ReadFile(cmd.OursPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(merged) != "A\nb\nC\n" {
		t.Errorf("a file without frontmatter should merge as plain text, got %q", merged)
	}
}

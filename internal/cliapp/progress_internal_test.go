package cliapp

import (
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// The shared wiring helper maps every command's callbacks onto one reporter: a
// zero-entry (warm) plan reports no phase, real embedding work reports indexing,
// and a freshen phase (syncing) is not clobbered by a subsequent zero-entry
// plan — so a text-only cross-repo read stays labeled syncing, never indexing.
func TestEmbedProgress_PhaseMapping(t *testing.T) {
	p := newEmbedProgress()

	p.onPlanned(0)
	if got := latestPhase(t, p); got != "" {
		t.Errorf("warm plan should report no phase; got %q", got)
	}

	p.connected(false).OnPhase(model.PhaseSyncing)
	if got := latestPhase(t, p); got != model.PhaseSyncing {
		t.Errorf("freshen should report syncing; got %q", got)
	}

	p.onPlanned(0) // a warm member fill must not overwrite syncing with indexing
	if got := latestPhase(t, p); got != model.PhaseSyncing {
		t.Errorf("zero-entry plan must not clobber syncing; got %q", got)
	}

	p.onPlanned(5) // real embedding work
	if got := latestPhase(t, p); got != model.PhaseIndexing {
		t.Errorf("real embedding should report indexing; got %q", got)
	}
}

func latestPhase(t *testing.T, p *embedProgress) model.Phase {
	t.Helper()
	snap, ok := p.reporter.Recv()
	if !ok {
		t.Fatal("reporter closed unexpectedly")
	}
	return snap.Phase
}

func TestEmbedProgressCountsPublishedEntries(t *testing.T) {
	p := newEmbedProgress()
	cmd := p.localBuild(false, nil)
	cmd.OnPlanned(2)
	cmd.OnBatchStart([]string{"a", "b"}, 32)
	cmd.OnEntryIndexed("a", 20)
	progress, ok := p.reporter.Recv()
	if !ok || progress.Done != 1 || progress.Total != 2 || progress.Unit != "entries" || !strings.Contains(progress.Note, "20 chunks published") {
		t.Fatalf("progress = %+v", progress)
	}
}

func TestEmbedProgressEmptyEntries(t *testing.T) {
	for _, tc := range []struct {
		name          string
		previousBatch bool
		repo          string
		want          string
	}{
		{name: "first local entry", want: "0 chunks published"},
		{name: "local entry after batch", previousBatch: true, want: "3 chunks published"},
		{name: "first connected entry", repo: "current", want: "current · 0 chunks published"},
		{name: "new repository after batch", previousBatch: true, repo: "current", want: "current · 3 chunks published"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newEmbedProgress()
			done := 1
			if tc.previousBatch {
				cmd := p.connected(false)
				if tc.repo != "" {
					cmd.OnRepoStart("previous")
				}
				cmd.OnPlanned(1)
				cmd.OnBatchStart([]string{"a"}, 3)
				cmd.OnEntryIndexed("a", 3)
				done++
			}
			if tc.repo != "" {
				cmd := p.connected(false)
				cmd.OnRepoStart(tc.repo)
				cmd.OnPhase(model.PhaseSyncing)
			}
			cmd := p.lazyFill()
			cmd.OnPlanned(1)
			cmd.OnEntryIndexed("empty", 0)
			progress, ok := p.reporter.Recv()
			if !ok || progress.Note != tc.want || progress.Done != done || progress.Total != done {
				t.Fatalf("progress = %+v, want note %q and %d completed entries", progress, tc.want, done)
			}
		})
	}
}

// A fill shorter than one interval prints nothing; a longer one prints a line
// per elapsed interval and a closing total.
func TestPlainFillProgress_PacesLines(t *testing.T) {
	tests := []struct {
		name  string
		steps []time.Duration
		want  []string
	}{
		{name: "short fill stays silent", steps: []time.Duration{time.Second, time.Second, time.Second}},
		{
			name:  "long fill reports progress and the total",
			steps: []time.Duration{fillProgressInterval, time.Second, fillProgressInterval},
			want: []string{
				"sdd: indexing for search: 1 of 3 entries embedded",
				"sdd: indexing for search: 3 of 3 entries embedded",
				"sdd: indexing for search: done, 3 of 3 entries embedded",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			clock := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
			cmd := newPlainFillProgress(&out, func() time.Time { return clock }).lazyFill()
			cmd.OnPlanned(len(tt.steps))
			for _, step := range tt.steps {
				clock = clock.Add(step)
				cmd.OnEntryIndexed("entry", 2)
			}
			cmd.OnComplete(len(tt.steps))
			var got []string
			if text := strings.TrimSpace(out.String()); text != "" {
				got = strings.Split(text, "\n")
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("progress lines should follow the interval, got %q", got)
			}
		})
	}
}

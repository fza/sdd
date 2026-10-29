package cliapp

import (
	"fmt"
	"io"
	"time"

	"github.com/networkteam/sdd/internal/cliout"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
)

// embedProgress bridges the embedding and cache-freshening command callbacks
// onto one cliout.Reporter, shared by `sdd index` and `sdd search`.
type embedProgress struct {
	reporter  *cliout.Reporter
	total     int
	curRepo   model.RepoID
	chunks    int
	batchNote string
}

func newEmbedProgress() *embedProgress {
	r := cliout.NewReporter()
	r.SetUnit("entries")
	return &embedProgress{reporter: r}
}

// onPlanned grows the running total (member work is only known after each cache
// is fresh) and declares the indexing phase once real embedding work is planned
// — an empty (warm) plan neither advances the bar nor arms a footer.
func (p *embedProgress) onPlanned(n int) {
	if n > 0 {
		p.reporter.SetPhase(model.PhaseIndexing)
	}
	p.total += n
	p.reporter.SetTotal(p.total)
}

func (p *embedProgress) onBatchStart(ids []string, chunks int) {
	note := embedNote(ids, chunks)
	if p.curRepo != "" {
		note = string(p.curRepo) + " · " + note
	}
	p.batchNote = note
	p.reporter.SetNote(fmt.Sprintf("%s · %d chunks published", note, p.chunks))
}

func (p *embedProgress) onEntryIndexed(_ string, chunks int) {
	p.chunks += chunks
	p.reporter.Add(1)
	prefix := p.batchNote
	if chunks == 0 {
		prefix = string(p.curRepo)
		p.batchNote = ""
	}
	note := fmt.Sprintf("%d chunks published", p.chunks)
	if prefix != "" {
		note = prefix + " · " + note
	}
	p.reporter.SetNote(note)
}

func (p *embedProgress) onRepoStart(id model.RepoID) {
	p.curRepo = id
	p.batchNote = ""
	p.reporter.SetNote("")
}

// onPhase maps a handler-reported freshening phase onto the footer and clears
// the stale embed note — a cache pull is not embedding any batch.
func (p *embedProgress) onPhase(ph model.Phase) {
	if ph == model.PhaseConnecting || ph == model.PhaseSyncing {
		p.batchNote = ""
		p.reporter.SetNote("")
	}
	p.reporter.SetPhase(ph)
}

// localBuild wires the local index build; onComplete carries the command's own
// summary totals back to the caller.
func (p *embedProgress) localBuild(force bool, onComplete func(indexed, skipped int)) *command.BuildIndexCmd {
	return &command.BuildIndexCmd{
		Force:          force,
		OnPlanned:      p.onPlanned,
		OnBatchStart:   p.onBatchStart,
		OnEntryIndexed: p.onEntryIndexed,
		OnComplete:     onComplete,
	}
}

// lazyFill wires the local lazy-fill that precedes a vector search.
func (p *embedProgress) lazyFill() *command.LazyFillIndexCmd {
	return &command.LazyFillIndexCmd{
		OnPlanned:      p.onPlanned,
		OnBatchStart:   p.onBatchStart,
		OnEntryIndexed: p.onEntryIndexed,
	}
}

// connected wires the connected-repo fill, keeping the footer label phase-true
// across the syncing → indexing transitions.
func (p *embedProgress) connected(force bool) *command.BuildConnectedIndexesCmd {
	return &command.BuildConnectedIndexesCmd{
		Force:          force,
		OnRepoStart:    p.onRepoStart,
		OnPlanned:      p.onPlanned,
		OnBatchStart:   p.onBatchStart,
		OnEntryIndexed: p.onEntryIndexed,
		OnPhase:        p.onPhase,
	}
}

// fillProgressInterval paces plain progress lines: a fill shorter than one
// interval prints nothing, a longer one shows it is alive.
const fillProgressInterval = 15 * time.Second

// plainFillProgress reports a long lazy-fill as plain stderr lines, for a
// non-interactive caller that gets no footer.
type plainFillProgress struct {
	writer   io.Writer
	now      func() time.Time
	total    int
	indexed  int
	last     time.Time
	reported bool
}

func newPlainFillProgress(writer io.Writer, now func() time.Time) *plainFillProgress {
	return &plainFillProgress{writer: writer, now: now, last: now()}
}

func (p *plainFillProgress) lazyFill() *command.LazyFillIndexCmd {
	return &command.LazyFillIndexCmd{
		OnPlanned: func(n int) { p.total += n },
		OnEntryIndexed: func(string, int) {
			p.indexed++
			if now := p.now(); now.Sub(p.last) >= fillProgressInterval {
				p.last = now
				p.reported = true
				_, _ = fmt.Fprintf(p.writer, "sdd: indexing for search: %d of %d entries embedded\n", p.indexed, p.total)
			}
		},
		OnComplete: func(indexed int) {
			if p.reported {
				_, _ = fmt.Fprintf(p.writer, "sdd: indexing for search: done, %d of %d entries embedded\n", indexed, p.total)
			}
		},
	}
}

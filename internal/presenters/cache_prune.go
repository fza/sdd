package presenters

import (
	"fmt"
	"io"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// CachePruneLine names the latest cache pruning run: when it finished, what
// it removed or how it failed. Empty before the first run.
func CachePruneLine(s model.CachePruneStatus) string {
	switch {
	case s.FinishedAt.IsZero() && s.StartedAt.IsZero():
		return ""
	case s.FinishedAt.Before(s.StartedAt):
		return fmt.Sprintf("Cache pruning: running since %s", s.StartedAt.Format(time.RFC3339))
	case s.Error != "":
		return fmt.Sprintf("Cache pruning: failed at %s: %s", s.FinishedAt.Format(time.RFC3339), s.Error)
	}
	return fmt.Sprintf("Cache pruning: last run %s, removed %s (%s)",
		s.FinishedAt.Format(time.RFC3339), CachePruneRemoved(s.Removed), HumanBytes(s.Removed.Bytes))
}

// CachePruneRemoved names what a run removed, and the unreadable session logs
// it kept.
func CachePruneRemoved(c model.CachePruneCounts) string {
	text := fmt.Sprintf("%d index versions, %d index stores, %d graph snapshots, %d clones, %d calibrations, %d leftovers, %d sessions, %d unused projects, %d gate records",
		c.Versions, c.Stores, c.Snapshots, c.Clones, c.Records, c.Leftovers, c.Sessions, c.Projects, c.GateRecords)
	if c.Unreadable > 0 {
		text += fmt.Sprintf("; kept %d unreadable session logs", c.Unreadable)
	}
	return text
}

// RenderCachePruneStatus writes CachePruneLine when there is a run to name.
func RenderCachePruneStatus(w io.Writer, s model.CachePruneStatus) {
	if line := CachePruneLine(s); line != "" {
		fmt.Fprintln(w, line)
	}
}

package presenters

import (
	"fmt"
	"io"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// CachePruneLine names the latest background cache pruning run: when it
// finished, what it removed or how it failed. Empty before the first run.
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
	text := fmt.Sprintf("%d index versions, %d index stores, %d graph snapshots, %d clones, %d sessions, %d unused projects, %d records, %d leftovers",
		c.Versions, c.Stores, c.Snapshots, c.Clones, c.Sessions, c.Projects, c.Records, c.Leftovers)
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

type cachePruneJSON struct {
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	Versions   int    `json:"versions"`
	Stores     int    `json:"stores"`
	Snapshots  int    `json:"snapshots"`
	Clones     int    `json:"clones"`
	Sessions   int    `json:"sessions"`
	Projects   int    `json:"projects"`
	Records    int    `json:"records"`
	Leftovers  int    `json:"leftovers"`
	Unreadable int    `json:"unreadable_sessions"`
	Bytes      int64  `json:"bytes"`
	Error      string `json:"error,omitempty"`
}

func cachePruneValue(s model.CachePruneStatus) *cachePruneJSON {
	if s.StartedAt.IsZero() && s.FinishedAt.IsZero() {
		return nil
	}
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	c := s.Removed
	return &cachePruneJSON{
		StartedAt: stamp(s.StartedAt), FinishedAt: stamp(s.FinishedAt),
		Versions: c.Versions, Stores: c.Stores, Snapshots: c.Snapshots, Clones: c.Clones,
		Sessions: c.Sessions, Projects: c.Projects, Records: c.Records, Leftovers: c.Leftovers, Unreadable: c.Unreadable,
		Bytes: c.Bytes, Error: s.Error,
	}
}

package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// HandEdit is a commit outside sdd's own that changed a committed entry
// (20261109-004506-d-cpt-udz, D5).
type HandEdit struct {
	Commit string
	Entry  string
	Time   time.Time
}

// HandEdits lists the commits since since that modified an entry file in
// the graph directory the adapter runs in and whose subject lacks sdd's own
// "sdd:" prefix. Entries are immutable, so such a change is a hand edit the
// graph has to recheck. Merge commits show no changes of their own and never
// count.
func (c CLI) HandEdits(ctx context.Context, since time.Time) ([]HandEdit, error) {
	out, err := c.command(ctx, "log", "--no-renames", "--diff-filter=M", "--relative", "--name-only",
		"--since="+strconv.FormatInt(since.Unix(), 10), "--format=%x00%H%x1f%ct%x1f%s", "--", ".").Output()
	if err != nil {
		return nil, fmt.Errorf("git log of hand edits: %w", err)
	}
	var edits []HandEdit
	for _, record := range strings.Split(string(out), "\x00") {
		lines := strings.Split(strings.TrimSpace(record), "\n")
		header := strings.SplitN(lines[0], "\x1f", 3)
		if len(header) != 3 || strings.HasPrefix(header[2], "sdd:") {
			continue
		}
		seconds, err := strconv.ParseInt(header[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("git log of hand edits: commit time %q: %w", header[1], err)
		}
		at := time.Unix(seconds, 0).UTC()
		if !at.After(since) {
			continue
		}
		for _, name := range lines[1:] {
			id, err := model.RelPathToID(strings.TrimSpace(name))
			if err != nil {
				continue
			}
			_, err = model.ParseID(id)
			if err != nil {
				continue
			}
			edits = append(edits, HandEdit{Commit: header[0], Entry: id, Time: at})
		}
	}
	return edits, nil
}

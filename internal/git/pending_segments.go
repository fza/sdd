package git

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// segmentPathspecs match the segments sdd shares through Git: those of every
// event channel and of the committed relevance ledger. A local channel's
// directory ignores itself, and an uncommitted ledger lives outside the
// repository, so the segments git lists are shared ones, and they ride the
// commit sdd is about to make instead of commits of their own
// (20261109-004506-d-cpt-udz, D17 and D19).
var segmentPathspecs = []string{
	":(top,glob)**/" + path.Join(model.SDDDirName, model.EventsDirName) + "/*/*.seg",
	":(top,glob)**/" + path.Join(model.SDDDirName, model.RelevanceDirName) + "/*.seg",
}

// PendingSegments returns the shared segments the next commit has to carry:
// new ones git neither tracks nor ignores, and tracked ones compaction
// removed, as paths relative to the directory the adapter's commands run in.
// Stage them with `git add --all`, which records a removal too.
func (c CLI) PendingSegments(ctx context.Context) ([]string, error) {
	args := append([]string{"ls-files", "--others", "--deleted", "--exclude-standard", "-z", "--"}, segmentPathspecs...)
	out, err := c.command(ctx, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files shared segments: %w", err)
	}
	var segments []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			segments = append(segments, name)
		}
	}
	return segments, nil
}

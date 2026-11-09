package git

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// eventSegmentsPathspec matches the segments of every event channel in the
// repository. A local channel's directory ignores itself, so the untracked
// segments git lists belong to git channels, and they ride the commit sdd is
// about to make instead of commits of their own (20261109-004506-d-cpt-udz,
// D19).
var eventSegmentsPathspec = ":(top,glob)**/" + path.Join(model.SDDDirName, model.EventsDirName) + "/*/*.seg"

// PendingEventSegments returns the event segments git neither tracks nor
// ignores, as paths relative to the directory the adapter's commands run in.
func (c CLI) PendingEventSegments(ctx context.Context) ([]string, error) {
	out, err := c.command(ctx, "ls-files", "--others", "--exclude-standard", "-z", "--", eventSegmentsPathspec).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files event segments: %w", err)
	}
	var segments []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			segments = append(segments, name)
		}
	}
	return segments, nil
}

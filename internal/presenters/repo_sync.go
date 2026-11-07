package presenters

import (
	"fmt"

	"github.com/networkteam/sdd/internal/model"
)

// RepoSyncOutcome renders one connected repo's sync outcome as `sdd repo
// sync` prints it, times in the local zone.
func RepoSyncOutcome(outcome model.RepoSyncOutcome) string {
	at := outcome.Next.Local().Format("15:04")
	switch outcome.State {
	case model.RepoSkipped:
		return fmt.Sprintf("%s: skipped (next %s)", outcome.RepoID, at)
	case model.RepoFailed:
		if outcome.Next.IsZero() {
			return fmt.Sprintf("%s: failed (%s)", outcome.RepoID, outcome.Reason)
		}
		return fmt.Sprintf("%s: failed (%s, retry %s)", outcome.RepoID, outcome.Reason, at)
	case model.RepoRefused:
		return fmt.Sprintf("%s: refused (%s)", outcome.RepoID, outcome.Reason)
	}
	return fmt.Sprintf("%s: %s", outcome.RepoID, outcome.State)
}

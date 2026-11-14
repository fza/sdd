package git

import "context"

// GCAuto lets git pack the repository when its own thresholds say so. It is
// local: in an sdd-owned clone it runs with lazy fetching barred, so it opens
// no connection.
func (c CLI) GCAuto(ctx context.Context) error {
	return c.run(ctx, "gc", "--auto", "--quiet")
}

package handlers

import (
	"context"
	"fmt"
)

// commitGraphOrFail is commitGraph whose error names a commit that did not
// land as a failed git commit and leaves a failure after a landed commit in
// afterGraphCommit's words.
func (h *Handler) commitGraphOrFail(ctx context.Context, message string, paths ...string) error {
	err := h.committer.Commit(message, paths...)
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return h.afterGraphCommit(ctx, message)
}

package handlers

import (
	"context"
	"fmt"
	"os"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
)

// MergeConfig merges `.sdd/config.yaml` per key for git (model.MergeConfigYAML).
// git's file is written only with a finished result.
func (h *Handler) MergeConfig(_ context.Context, cmd *command.MergeConfigCmd) error {
	var versions [3][]byte
	for i, path := range []string{cmd.BasePath, cmd.OursPath, cmd.TheirsPath} {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		versions[i] = content
	}
	merged, notices, err := model.MergeConfigYAML(versions[0], versions[1], versions[2])
	if err != nil {
		return fmt.Errorf("merging config.yaml: %w", err)
	}
	staged := cmd.OursPath + ".sdd-merge"
	err = os.WriteFile(staged, merged, 0o644)
	if err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("writing the merge result: %w", err)
	}
	err = os.Rename(staged, cmd.OursPath)
	if err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("writing the merge result: %w", err)
	}
	for _, notice := range notices {
		if cmd.OnNotice != nil {
			cmd.OnNotice(notice)
		}
	}
	return nil
}

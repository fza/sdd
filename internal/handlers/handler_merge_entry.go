package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
)

// ErrEntryConflict reports that MergeEntry left conflict markers in the
// merged file, which git reads as a conflicted merge.
var ErrEntryConflict = errors.New("entry merge left conflict markers")

// MergeEntry merges one graph entry file for git: title and summary resolve
// on their own (model.ResolveMergedField), every other line merges as git's
// text merge would. A version whose frontmatter cannot be read merges as
// plain text throughout.
func (h *Handler) MergeEntry(ctx context.Context, cmd *command.MergeEntryCmd) error {
	if h.fileMerger == nil {
		return errors.New("merge-entry: no file merger configured")
	}
	base, err := os.ReadFile(cmd.BasePath)
	if err != nil {
		return fmt.Errorf("reading the base version: %w", err)
	}
	ours, err := os.ReadFile(cmd.OursPath)
	if err != nil {
		return fmt.Errorf("reading our version: %w", err)
	}
	theirs, err := os.ReadFile(cmd.TheirsPath)
	if err != nil {
		return fmt.Errorf("reading their version: %w", err)
	}

	versions := model.ResolveDerivedFields([3]string{string(base), string(ours), string(theirs)})

	// git's file is written only with a finished result, so a failure leaves
	// it as git handed it over for a fallback merge (20261030-093341-d-tac-t6j).
	var paths [3]string
	for i, name := range []string{"base", "ours", "theirs"} {
		path, clean, err := writeTempVersion(name, versions[i])
		if err != nil {
			return err
		}
		defer clean()
		paths[i] = path
	}

	conflicted, err := h.fileMerger.MergeFile(ctx, paths[1], paths[0], paths[2], cmd.MarkerSize)
	if err != nil {
		return fmt.Errorf("merging %s: %w", cmd.OursPath, err)
	}
	merged, err := os.ReadFile(paths[1])
	if err != nil {
		return fmt.Errorf("reading the merge result: %w", err)
	}
	if !conflicted {
		merged = []byte(model.DropRepeatedFrontmatterKeys(string(merged), model.MergeResolvedKeys))
	}
	staged := cmd.OursPath + ".sdd-merge"
	if err := os.WriteFile(staged, merged, 0o644); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("writing the merge result: %w", err)
	}
	if err := os.Rename(staged, cmd.OursPath); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("writing the merge result: %w", err)
	}
	if conflicted {
		return ErrEntryConflict
	}
	return nil
}

func writeTempVersion(name, content string) (path string, clean func(), err error) {
	file, err := os.CreateTemp("", "sdd-merge-entry-"+name+"-*")
	if err != nil {
		return "", nil, fmt.Errorf("staging the %s version: %w", name, err)
	}
	clean = func() { _ = os.Remove(file.Name()) }
	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()
		clean()
		return "", nil, fmt.Errorf("staging the %s version: %w", name, err)
	}
	if err := file.Close(); err != nil {
		clean()
		return "", nil, fmt.Errorf("staging the %s version: %w", name, err)
	}
	return file.Name(), clean, nil
}

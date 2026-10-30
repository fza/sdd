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

	versions := [3]string{string(base), string(ours), string(theirs)}
	resolveDerivedFields(&versions)

	if err := os.WriteFile(cmd.OursPath, []byte(versions[1]), 0o644); err != nil {
		return fmt.Errorf("writing our version: %w", err)
	}
	basePath, cleanBase, err := writeTempVersion("base", versions[0])
	if err != nil {
		return err
	}
	defer cleanBase()
	theirsPath, cleanTheirs, err := writeTempVersion("theirs", versions[2])
	if err != nil {
		return err
	}
	defer cleanTheirs()

	conflicted, err := h.fileMerger.MergeFile(ctx, cmd.OursPath, basePath, theirsPath, cmd.MarkerSize)
	if err != nil {
		return fmt.Errorf("merging %s: %w", cmd.OursPath, err)
	}
	if conflicted {
		return ErrEntryConflict
	}
	return nil
}

// resolveDerivedFields writes each resolved title and summary into every
// version stating that key, so the text merge sees no difference there.
func resolveDerivedFields(versions *[3]string) {
	var fields [3]map[string]model.FrontmatterField
	for i, content := range versions {
		parsed, ok := model.FrontmatterFields(content, model.MergeResolvedKeys)
		if !ok {
			return
		}
		fields[i] = parsed
	}
	for _, key := range model.MergeResolvedKeys {
		resolved := model.ResolveMergedField(fields[0][key], fields[1][key], fields[2][key])
		for i := range versions {
			versions[i] = model.ReplaceFrontmatterField(versions[i], key, resolved.Lines)
		}
	}
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

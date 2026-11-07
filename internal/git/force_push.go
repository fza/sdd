package git

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
)

// An upstream force push in the sdd-owned graph clone
// (20261106-202055-d-tac-eyl): the clone follows the rewrite locally, keeps
// sdd's title and summary changes to entries that survive it, and sets aside
// the entries and attachments sdd had not pushed.

const (
	fetchedTipFile  = "sdd-upstream"
	setAsideDirName = "sdd-set-aside"
	setAsideTips    = "tips"
	setAsideFiles   = "files"
)

// fetchedTip is the upstream tip the clone last fetched: the one noted, else
// the tracking ref as it stands, empty before the first fetch.
func (c CLI) fetchedTip(ctx context.Context) (string, error) {
	noted, err := readOptional(filepath.Join(c.Dir, ".git", fetchedTipFile))
	if err != nil {
		return "", err
	}
	if noted = strings.TrimSpace(noted); noted != "" {
		return noted, nil
	}
	tip, err := c.RevParse(ctx, "@{u}")
	if err != nil {
		return "", nil
	}
	return tip, nil
}

func (c CLI) noteFetchedTip(ctx context.Context) error {
	tip, err := c.RevParse(ctx, "@{u}")
	if err != nil {
		return nil
	}
	return writeIfChanged(filepath.Join(c.Dir, ".git", fetchedTipFile), tip+"\n")
}

// followForcePush moves the clone onto its upstream when the upstream no
// longer contains previous, the tip fetched before.
func (c CLI) followForcePush(ctx context.Context, previous string) error {
	upstream, err := c.RevParse(ctx, "@{u}")
	if err != nil || previous == "" || previous == upstream {
		return c.noteFetchedTip(ctx)
	}
	err = exec.CommandContext(ctx, "git", c.at("merge-base", "--is-ancestor", previous, upstream)...).Run()
	if err == nil {
		return c.noteFetchedTip(ctx)
	}
	if code := errExitCode(err); code != 1 && code != 128 {
		return fmt.Errorf("git merge-base --is-ancestor %s %s: %w", previous, upstream, err)
	}
	err = c.recoverForcePush(ctx, previous, upstream)
	if err != nil {
		return fmt.Errorf("following the force push to %s: %w", upstream, err)
	}
	slogutils.FromContext(ctx).Info(model.ForcePushNotice(previous, upstream))
	return c.noteFetchedTip(ctx)
}

type derivedEdit struct{ path, before, after string }

func (c CLI) recoverForcePush(ctx context.Context, previous, upstream string) error {
	recorded, err := c.recordedCommits()
	if err != nil {
		return err
	}
	listed, err := c.output(ctx, "rev-list", "--reverse", "--no-merges", previous+"..HEAD")
	if err != nil {
		return err
	}
	var added []string
	var edits []derivedEdit
	for commit := range strings.FieldsSeq(listed) {
		if !slices.Contains(recorded, commit) {
			continue
		}
		changed, err := c.output(ctx, "diff-tree", "--no-commit-id", "-r", "--no-renames", "--name-status", commit+"^", commit)
		if err != nil {
			return err
		}
		for line := range strings.SplitSeq(changed, "\n") {
			status, file, found := strings.Cut(line, "\t")
			switch {
			case !found:
			case status == "A" && !slices.Contains(added, file):
				added = append(added, file)
			case status == "M" && model.IsEntryPath(file) && !slices.Contains(added, file):
				before, err := c.contentAt(ctx, commit+"^", file)
				if err != nil {
					return err
				}
				after, err := c.contentAt(ctx, commit, file)
				if err != nil {
					return err
				}
				edits = append(edits, derivedEdit{path: file, before: before, after: after})
			}
		}
	}
	unpushed := map[string]string{}
	for _, file := range added {
		content, err := c.contentAt(ctx, "HEAD", file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		unpushed[file] = content
	}

	err = run(ctx, c.Dir, "reset", "--quiet", "--hard", upstream)
	if err != nil {
		return err
	}
	err = c.setAside(previous, upstream, unpushed)
	if err != nil {
		return err
	}
	var rederived []string
	for _, edit := range edits {
		target := filepath.Join(c.Dir, filepath.FromSlash(edit.path))
		current, err := os.ReadFile(target)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading %s: %w", edit.path, err)
		}
		next := model.RederiveFields(edit.before, edit.after, string(current))
		if next == string(current) {
			continue
		}
		err = os.WriteFile(target, []byte(next), 0o644)
		if err != nil {
			return fmt.Errorf("writing %s: %w", edit.path, err)
		}
		if !slices.Contains(rederived, edit.path) {
			rederived = append(rederived, edit.path)
		}
	}
	if len(rederived) == 0 {
		return nil
	}
	return c.Commit("sdd: re-derive titles and summaries after an upstream force push", c.absolute(rederived)...)
}

// contentAt reads file as rev holds it, fs.ErrNotExist when rev holds none.
func (c CLI) contentAt(ctx context.Context, rev, file string) (string, error) {
	err := exec.CommandContext(ctx, "git", c.at("cat-file", "-e", rev+":"+file)...).Run()
	if err != nil {
		if errExitCode(err) == 128 {
			return "", fs.ErrNotExist
		}
		return "", fmt.Errorf("git cat-file -e %s:%s: %w", rev, file, err)
	}
	out, err := exec.CommandContext(ctx, "git", c.at("show", rev+":"+file)...).Output()
	if err != nil {
		return "", fmt.Errorf("git show %s:%s: %w", rev, file, err)
	}
	return string(out), nil
}

func (c CLI) absolute(files []string) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, filepath.Join(c.Dir, filepath.FromSlash(file)))
	}
	return paths
}

func (c CLI) setAsideDir() string {
	return filepath.Join(c.Dir, ".git", setAsideDirName)
}

// setAside keeps files beside the clone, noting the rewrite they came from;
// files the rewritten upstream holds stay with it.
func (c CLI) setAside(previous, upstream string, files map[string]string) error {
	if len(files) == 0 {
		return nil
	}
	for file, content := range files {
		_, err := os.Stat(filepath.Join(c.Dir, filepath.FromSlash(file)))
		if err == nil {
			continue
		}
		err = writeIfChanged(filepath.Join(c.setAsideDir(), setAsideFiles, filepath.FromSlash(file)), content)
		if err != nil {
			return err
		}
	}
	return writeIfChanged(filepath.Join(c.setAsideDir(), setAsideTips), previous+" "+upstream+"\n")
}

// SetAside lists what upstream force pushes set aside in the clone.
func (c CLI) SetAside(_ context.Context) (model.SetAside, error) {
	files := filepath.Join(c.setAsideDir(), setAsideFiles)
	tips, err := readOptional(filepath.Join(c.setAsideDir(), setAsideTips))
	if err != nil {
		return model.SetAside{}, err
	}
	previous, upstream, _ := strings.Cut(strings.TrimSpace(tips), " ")
	found := model.SetAside{Old: previous, New: upstream, Dir: files}
	err = filepath.WalkDir(files, func(file string, entry fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return fs.SkipAll
		}
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(files, file)
		if err != nil {
			return err
		}
		if model.IsEntryPath(filepath.ToSlash(relative)) {
			found.Entries = append(found.Entries, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return model.SetAside{}, fmt.Errorf("listing the set-aside entries: %w", err)
	}
	slices.Sort(found.Entries)
	return found, nil
}

// ReaddSetAside returns a set-aside entry and its attachments to the graph
// and commits them; the push is the sync round's.
func (c CLI) ReaddSetAside(ctx context.Context, entry string) error {
	files := filepath.Join(c.setAsideDir(), setAsideFiles)
	attachments := strings.TrimSuffix(entry, ".md")
	var returned []string
	err := filepath.WalkDir(files, func(file string, item fs.DirEntry, err error) error {
		if err != nil || item.IsDir() {
			return err
		}
		relative, err := filepath.Rel(files, file)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative != entry && !strings.HasPrefix(relative, attachments+"/") {
			return nil
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading the set-aside %s: %w", relative, err)
		}
		err = writeIfChanged(filepath.Join(c.Dir, filepath.FromSlash(relative)), string(content))
		if err != nil {
			return err
		}
		returned = append(returned, relative)
		return nil
	})
	if err != nil {
		return err
	}
	if !slices.Contains(returned, entry) {
		return fmt.Errorf("no set-aside entry %s", entry)
	}
	id, err := model.EntryPathID(entry)
	if err != nil {
		return err
	}
	err = c.Commit("sdd: re-add "+id+" after an upstream force push", c.absolute(returned)...)
	if err != nil {
		return err
	}
	for _, file := range returned {
		err = os.Remove(filepath.Join(files, filepath.FromSlash(file)))
		if err != nil {
			return fmt.Errorf("removing the re-added %s from the set-aside files: %w", file, err)
		}
	}
	return nil
}

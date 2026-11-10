package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/model"
)

// The sdd-owned graph clone (20261106-202055-d-tac-eyl). A clone is
// recognised by the recorded-commits file in its git directory, so every
// adapter built on its root behaves as the clone needs: merge drivers passed
// per call, commits recorded, protocol switched on failure.

const (
	sshCommand      = "ssh -o BatchMode=yes -o ConnectTimeout=5"
	remoteKey       = "sdd.remote"
	remoteSinceKey  = "sdd.remoteSince"
	defaultBranch   = "main"
	trackingPrefix  = "refs/remotes/origin/"
	cloneStagingExt = model.GraphCloneStagingSuffix
)

// cloneExcludeLines keep sdd's own files in the clone out of git and out of
// the upkeep's clean.
var cloneExcludeLines = []string{model.GraphCloneExcludeLine, "/.sdd/tmp/", "/.sdd/config.local.yaml"}

// IsGraphClone reports whether dir is the root of an sdd-owned graph clone.
func IsGraphClone(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git", model.GraphCloneCommits))
	return err == nil
}

func (c CLI) graphClone() bool { return IsGraphClone(c.Dir) }

// GraphClones is the repos.GraphCloneGit the clone owner works through.
type GraphClones struct{}

func (GraphClones) CloneGraph(ctx context.Context, repoID model.RepoID, source model.CloneSource, dir string, cone []string) error {
	return CloneGraph(ctx, repoID, source, dir, cone)
}

// SparseCone lists the directories the clone at dir checks out.
func (GraphClones) SparseCone(ctx context.Context, dir string) ([]string, error) {
	out, err := CLI{Dir: dir}.output(ctx, "sparse-checkout", "list")
	if err != nil {
		return nil, err
	}
	var cone []string
	for line := range strings.SplitSeq(out, "\n") {
		if line != "" {
			cone = append(cone, line)
		}
	}
	return cone, nil
}

// SetSparseCone checks out exactly cone in the clone at dir.
func (GraphClones) SetSparseCone(ctx context.Context, dir string, cone []string) error {
	return CLI{Dir: dir}.run(ctx, append([]string{"sparse-checkout", "set", "--cone"}, cone...)...)
}

func (GraphClones) KeepGraphClone(ctx context.Context, dir string, routing []string) error {
	return CLI{Dir: dir}.KeepGraphClone(ctx, routing)
}

func (GraphClones) PushGraphClone(ctx context.Context, dir string) error {
	return CLI{Dir: dir}.Push(ctx)
}

// CommitsWaiting reports whether the clone at dir holds commits its upstream
// lacks: any, on a born branch with no upstream yet.
func (GraphClones) CommitsWaiting(ctx context.Context, dir string) (bool, error) {
	c := CLI{Dir: dir}
	born, err := c.hasHead(ctx)
	if err != nil || !born {
		return false, err
	}
	upstream, err := c.UpstreamRef(ctx)
	if err != nil {
		return false, err
	}
	if upstream == "" {
		return true, nil
	}
	ahead, err := c.CountCommits(ctx, upstream+"..HEAD", "")
	return ahead > 0, err
}

// RemoteURL is the URL of the protocol remote the clone at dir uses.
func (GraphClones) RemoteURL(ctx context.Context, dir string) (string, error) {
	return CLI{Dir: dir}.UpstreamURL(ctx)
}

// ExpandURL is the package's ExpandURL, for the repos.GraphCloneGit surface.
func (GraphClones) ExpandURL(ctx context.Context, url string) (string, error) {
	return ExpandURL(ctx, url)
}

// ErrGraphUnreachable reports a graph repository none of its remotes reached.
var ErrGraphUnreachable = errors.New("graph repository unreachable")

// CloneGraph makes the sdd-owned clone of repoID at dir: blobless, sparse
// with cone, fetching from source. Its remotes share refs/remotes/origin: ssh
// and https, source.First tried before the other protocol, or source.URL
// alone. An empty remote leaves the branch unborn on main. Nothing is left at
// dir when it fails.
func CloneGraph(ctx context.Context, repoID model.RepoID, source model.CloneSource, dir string, cone []string) error {
	_, err := admittedEnv(ctx, "clone")
	if err != nil {
		return err
	}
	remotes, err := cloneRemotes(repoID, source)
	if err != nil {
		return err
	}
	staging := dir + cloneStagingExt
	err = os.RemoveAll(staging)
	if err != nil {
		return fmt.Errorf("clearing %s: %w", staging, err)
	}
	err = os.MkdirAll(filepath.Dir(dir), 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dir), err)
	}
	err = buildGraphClone(ctx, staging, remotes, cone)
	if err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("cloning %s: %w", cloneName(repoID, source), err)
	}
	err = os.Rename(staging, dir)
	if err != nil {
		_ = os.RemoveAll(staging)
		return fmt.Errorf("placing the clone of %s: %w", cloneName(repoID, source), err)
	}
	return nil
}

func cloneName(repoID model.RepoID, source model.CloneSource) string {
	if repoID != "" {
		return string(repoID)
	}
	return source.URL
}

type cloneRemote struct{ name, url string }

// cloneRemotes lists the remotes of a clone from source in the order its
// first fetch tries them.
func cloneRemotes(repoID model.RepoID, source model.CloneSource) ([]cloneRemote, error) {
	if source.URL != "" {
		return []cloneRemote{{name: model.SingleRemote, url: source.URL}}, nil
	}
	sshURL, httpsURL, err := model.GraphRemoteURLs(repoID)
	if err != nil {
		return nil, err
	}
	if source.First == model.ProtocolHTTPS {
		return []cloneRemote{{model.ProtocolHTTPS, httpsURL}, {model.ProtocolSSH, sshURL}}, nil
	}
	return []cloneRemote{{model.ProtocolSSH, sshURL}, {model.ProtocolHTTPS, httpsURL}}, nil
}

func buildGraphClone(ctx context.Context, dir string, remotes []cloneRemote, cone []string) error {
	err := run(ctx, "", "init", "--quiet", dir)
	if err != nil {
		return err
	}
	at := CLI{Dir: dir}
	settings := [][2]string{{"core.sshCommand", sshCommand}}
	for _, remote := range remotes {
		settings = append(settings,
			[2]string{"remote." + remote.name + ".url", remote.url},
			[2]string{"remote." + remote.name + ".fetch", "+refs/heads/*:" + trackingPrefix + "*"})
	}
	for _, setting := range settings {
		err = at.setConfig(ctx, setting[0], setting[1])
		if err != nil {
			return err
		}
	}
	var failures []error
	working := ""
	for _, remote := range remotes {
		_, fetchErr := at.runRemote(ctx, "fetch", "--quiet", "--filter=blob:none", remote.name)
		if fetchErr == nil {
			working = remote.name
			break
		}
		failures = append(failures, fmt.Errorf("%s: %w", remote.name, fetchErr))
	}
	if working == "" {
		return errors.Join(append([]error{ErrGraphUnreachable}, failures...)...)
	}
	branch, err := at.remoteDefaultBranch(ctx, working)
	if err != nil {
		return err
	}
	// The checkout fetches the cone's contents inside the clone's admission.
	env := cloneEnv(ctx)
	err = runEnv(ctx, env, dir, append([]string{"sparse-checkout", "set", "--cone"}, cone...)...)
	if err != nil {
		return err
	}
	if branch == "" {
		branch = defaultBranch
		err = runEnv(ctx, env, dir, "symbolic-ref", "HEAD", "refs/heads/"+branch)
	} else {
		err = runEnv(ctx, env, dir, "checkout", "--quiet", "--no-track", "-b", branch, trackingPrefix+branch)
	}
	if err != nil {
		return err
	}
	err = at.setConfig(ctx, "branch."+branch+".merge", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	err = at.useProtocol(ctx, branch, working, time.Now())
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".git", model.GraphCloneCommits), nil, 0o644)
}

// remoteDefaultBranch is the branch the remote's HEAD names, empty for an
// empty repository.
func (c CLI) remoteDefaultBranch(ctx context.Context, remote string) (string, error) {
	out, err := c.runRemote(ctx, "ls-remote", "--symref", remote, "HEAD")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(out, "\n") {
		ref, ok := strings.CutPrefix(line, "ref: refs/heads/")
		if ok {
			branch, _, _ := strings.Cut(ref, "\t")
			return branch, nil
		}
	}
	return "", nil
}

// useProtocol makes protocol the remote the branch fetches, pushes and
// lazily loads file contents from, recording it and since when.
func (c CLI) useProtocol(ctx context.Context, branch, protocol string, now time.Time) error {
	other := model.ProtocolSSH
	if protocol == model.ProtocolSSH {
		other = model.ProtocolHTTPS
	}
	recorded, err := c.localConfig(ctx, remoteKey)
	if err != nil {
		return err
	}
	settings := [][2]string{
		{"branch." + branch + ".remote", protocol},
		{"remote." + protocol + ".promisor", "true"},
		{"remote." + protocol + ".partialclonefilter", "blob:none"},
	}
	if recorded != protocol {
		settings = append(settings, [2]string{remoteKey, protocol}, [2]string{remoteSinceKey, now.UTC().Format(time.RFC3339)})
	}
	for _, setting := range settings {
		err = c.setConfig(ctx, setting[0], setting[1])
		if err != nil {
			return err
		}
	}
	for _, key := range []string{"remote." + other + ".promisor", "remote." + other + ".partialclonefilter"} {
		err = c.unsetConfig(ctx, key)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) setConfig(ctx context.Context, key, value string) error {
	out, err := c.command(ctx, "config", "--local", key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config %s: %s (%w)", key, strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (c CLI) unsetConfig(ctx context.Context, key string) error {
	out, err := c.command(ctx, "config", "--local", "--unset-all", key).CombinedOutput()
	if err == nil || errExitCode(err) == 5 {
		return nil
	}
	return fmt.Errorf("git config --unset %s: %s (%w)", key, strings.TrimSpace(string(out)), err)
}

// runRemote runs a git command reaching a remote: detached from the
// terminal, never prompting, ssh in batch mode with a connect timeout.
func (c CLI) runRemote(ctx context.Context, args ...string) (string, error) {
	env, err := admittedEnv(ctx, args[0], "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND="+sshCommand)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, detachedTimeout)
	defer cancel()
	cmd := c.command(ctx, append([]string{"-c", "fetch.unpackLimit=1"}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("git %s: timed out after %s", args[0], detachedTimeout)
	}
	if err != nil {
		return string(out), fmt.Errorf("git %s: %s (%w)", args[0], strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// withRemote runs operation against the clone's remotes in protocol order,
// switching the recorded protocol when another one worked. A failed ssh retry
// after a day on https renews the https record, so the retry costs one
// timeout a day.
func (c CLI) withRemote(ctx context.Context, operation func(remote string) error) error {
	branch, err := c.CurrentBranch(ctx)
	if err != nil {
		return err
	}
	if branch == "" {
		return errors.New("the graph clone is on no branch")
	}
	recorded, err := c.localConfig(ctx, remoteKey)
	if err != nil {
		return err
	}
	sinceText, err := c.localConfig(ctx, remoteSinceKey)
	if err != nil {
		return err
	}
	since, _ := time.Parse(time.RFC3339, sinceText)
	now := time.Now()
	var failures []error
	for _, protocol := range model.ProtocolOrder(recorded, since, now) {
		opErr := operation(protocol)
		if opErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", protocol, opErr))
			continue
		}
		if protocol == recorded && len(failures) > 0 {
			err = c.setConfig(ctx, remoteSinceKey, now.UTC().Format(time.RFC3339))
			if err != nil {
				return err
			}
		}
		return c.useProtocol(ctx, branch, protocol, now)
	}
	return errors.Join(append([]error{ErrGraphUnreachable}, failures...)...)
}

// fetchClone fetches the clone's upstream over the working protocol with the
// full contents of the new commits, and backfills the contents a merge of the
// upstream still lacks, so the merge runs without the remote
// (20261107-224549-d-tac-r9q, 20261108-104247-d-tac-k6f).
func (c CLI) fetchClone(ctx context.Context) error {
	previous, err := c.fetchedTip(ctx)
	if err != nil {
		return err
	}
	err = c.withRemote(ctx, func(remote string) error {
		_, err := c.runRemote(ctx, "fetch", "--quiet", "--no-filter", remote)
		return err
	})
	if err != nil {
		return err
	}
	err = c.followForcePush(ctx, previous)
	if err != nil {
		return err
	}
	return c.backfillUpstream(ctx)
}

// backfillUpstream fetches the contents inside the sparse set that the
// commits between HEAD and the upstream, and their boundary, lack. It opens
// no connection when nothing is missing.
func (c CLI) backfillUpstream(ctx context.Context) error {
	if !lazyFetchBar() {
		return nil
	}
	born, err := c.hasHead(ctx)
	if err != nil || !born {
		return err
	}
	upstream, err := c.UpstreamRef(ctx)
	if err != nil || upstream == "" {
		return err
	}
	return c.run(ctx, "backfill", "--sparse", "HEAD.."+upstream)
}

// pullClone fetches and merges the clone's upstream.
func (c CLI) pullClone(ctx context.Context) (string, error) {
	err := c.fetchClone(ctx)
	if err != nil {
		return "", err
	}
	upstream, err := c.UpstreamRef(ctx)
	if err != nil {
		return "", err
	}
	if upstream == "" {
		return "", errors.New("the graph clone's branch has no upstream yet")
	}
	err = c.mergeClone(ctx, upstream, model.GraphMergeMessage(upstream))
	if err != nil {
		return "", err
	}
	return "merged " + upstream, nil
}

// pushClone publishes HEAD to the branch's upstream over the working
// protocol.
func (c CLI) pushClone(ctx context.Context) error {
	branch, err := c.CurrentBranch(ctx)
	if err != nil {
		return err
	}
	merge, err := c.localConfig(ctx, "branch."+branch+".merge")
	if err != nil {
		return err
	}
	if merge == "" {
		merge = "refs/heads/" + branch
	}
	err = c.withRemote(ctx, func(remote string) error {
		_, err := c.runRemote(ctx, "push", "--quiet", remote, "HEAD:"+merge)
		return err
	})
	if err != nil {
		return err
	}
	return c.noteFetchedTip(ctx)
}

// KeepGraphClone restores the clone to what only sdd made: tracked files
// restored, untracked ones removed, commits sdd did not record dropped with
// its own replayed on top of upstream, the marker and its exclude line in
// place. A non-nil routing is written last into .git/info/attributes.
// Everything happens without notice (20261106-202055-d-tac-eyl).
func (c CLI) KeepGraphClone(ctx context.Context, routing []string) error {
	gitDir := filepath.Join(c.Dir, ".git")
	for _, state := range []struct{ head, abort string }{{"MERGE_HEAD", "merge"}, {"CHERRY_PICK_HEAD", "cherry-pick"}} {
		_, statErr := os.Stat(filepath.Join(gitDir, state.head))
		if statErr == nil {
			_ = c.run(ctx, state.abort, "--abort")
		}
	}
	_, statErr := os.Stat(filepath.Join(gitDir, "HEAD"))
	if statErr != nil {
		return fmt.Errorf("%s holds no graph clone: %w", c.Dir, statErr)
	}
	born, err := c.hasHead(ctx)
	if err != nil {
		return err
	}
	if born {
		err = c.run(ctx, "reset", "--quiet", "--hard")
		if err != nil {
			return err
		}
	}
	err = c.run(ctx, "clean", "-f", "-d", "--quiet")
	if err != nil {
		return err
	}
	// The local layer of a graph_repo project is the code repository's; one
	// left in the clone, ignored and so spared by the clean, never counts.
	err = os.Remove(filepath.Join(c.Dir, model.SDDDirName, "config.local.yaml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing the clone's local config: %w", err)
	}
	err = writeIfChanged(filepath.Join(c.Dir, model.GraphCloneMarker), model.GraphCloneMarkerText)
	if err != nil {
		return err
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	exclude, err := readOptional(excludePath)
	if err != nil {
		return err
	}
	for _, line := range cloneExcludeLines {
		exclude = model.EnsureExcludeLine(exclude, line)
	}
	err = writeIfChanged(excludePath, exclude)
	if err != nil {
		return err
	}
	if routing != nil {
		attributesPath := filepath.Join(gitDir, "info", "attributes")
		attributes, err := readOptional(attributesPath)
		if err != nil {
			return err
		}
		err = writeIfChanged(attributesPath, model.RouteMergeAttributes(attributes, routing))
		if err != nil {
			return err
		}
	}
	// A force push fetched before an interrupted round is followed before
	// the upkeep would replay every commit of sdd's onto it.
	previous, err := readOptional(filepath.Join(gitDir, fetchedTipFile))
	if err != nil {
		return err
	}
	if previous = strings.TrimSpace(previous); previous != "" {
		err = c.followForcePush(ctx, previous)
		if lacksContents(err) {
			// Fetched without contents and barred from fetching them here:
			// the next fetch follows inside its admission, so the upkeep
			// stops before replaying sdd's commits onto the rewrite.
			slogutils.FromContext(ctx).Debug("clone: force push left to the next fetch", "dir", c.Dir)
			return c.run(ctx, "reset", "--quiet", "--hard")
		}
		if err != nil {
			return err
		}
	}
	return c.dropUnrecordedCommits(ctx)
}

// lacksContents reports whether err is git refusing to fetch contents the
// clone lacks, under the lazy fetch bar.
func lacksContents(err error) bool {
	return err != nil && strings.Contains(err.Error(), "lazy fetching disabled")
}

func (c CLI) hasHead(ctx context.Context) (bool, error) {
	err := c.command(ctx, "rev-parse", "--verify", "--quiet", "HEAD").Run()
	if err == nil {
		return true, nil
	}
	if errExitCode(err) == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git rev-parse HEAD: %w", err)
}

func (c CLI) dropUnrecordedCommits(ctx context.Context) error {
	upstream, err := c.RevParse(ctx, "@{u}")
	if err != nil {
		return nil
	}
	out, err := c.output(ctx, "rev-list", "--reverse", "--parents", upstream+"..HEAD")
	if err != nil {
		return err
	}
	var ahead []model.CloneCommit
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		ahead = append(ahead, model.CloneCommit{Hash: fields[0], Merge: len(fields) > 2})
	}
	recorded, err := c.recordedCommits()
	if err != nil {
		return err
	}
	plan := model.PlanCloneReplay(ahead, recorded)
	if !plan.Reset {
		return nil
	}
	err = c.run(ctx, "reset", "--quiet", "--hard", upstream)
	if err != nil {
		return err
	}
	for _, hash := range plan.Replay {
		err = c.runMerging(ctx, "cherry-pick", "--allow-empty", hash)
		if err != nil {
			_ = c.run(ctx, "cherry-pick", "--abort")
			return fmt.Errorf("replaying sdd's commit %s onto %s: %w", hash, upstream, err)
		}
		err = c.RecordGraphCloneCommit(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c CLI) recordedCommits() ([]string, error) {
	content, err := readOptional(filepath.Join(c.Dir, ".git", model.GraphCloneCommits))
	if err != nil {
		return nil, err
	}
	return model.ParseCloneCommits(content), nil
}

// RecordGraphCloneCommit records HEAD as a commit sdd made in the clone at
// Dir; outside a clone it does nothing.
func (c CLI) RecordGraphCloneCommit(ctx context.Context) error {
	if !c.graphClone() {
		return nil
	}
	head, err := c.RevParse(ctx, "HEAD")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(c.Dir, ".git", model.GraphCloneCommits), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("recording commit %s: %w", head, err)
	}
	_, err = file.WriteString(head + "\n")
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("recording commit %s: %w", head, err)
	}
	if closeErr != nil {
		return fmt.Errorf("recording commit %s: %w", head, closeErr)
	}
	return nil
}

// driverArgs pass the merge drivers per call: the clone's config holds none,
// and the running binary names itself, so the drivers follow it when it moves.
func driverArgs() ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locating the sdd binary for the merge drivers: %w", err)
	}
	quoted := "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	entry := quoted + " merge-entry %O %A %B %L %P; s=$?; [ $s -eq 0 ] && exit 0; [ $s -eq 10 ] && exit 1; " +
		"git merge-file -L ours -L base -L theirs --marker-size=%L %A %O %B"
	config := quoted + " merge-config %O %A %B %P"
	return []string{
		"-c", "merge." + model.EntryMergeDriver + ".driver=" + entry,
		"-c", "merge." + model.ConfigMergeDriver + ".driver=" + config,
	}, nil
}

// runMerging runs a git command that may merge files, with the drivers
// passed, relaying the notices the drivers print.
func (c CLI) runMerging(ctx context.Context, args ...string) error {
	drivers, err := driverArgs()
	if err != nil {
		return err
	}
	full := c.at(append(drivers, args...)...)
	out, err := runDetachedEnv(ctx, c.localEnv(ctx), full...)
	relayNotices(ctx, string(out))
	if err != nil {
		return fmt.Errorf("git %s: %s (%w)", args[0], strings.TrimSpace(string(out)), err)
	}
	return nil
}

func relayNotices(ctx context.Context, output string) {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		_, notice, found := strings.Cut(scanner.Text(), model.MergeNoticePrefix)
		if found {
			slogutils.FromContext(ctx).Info("sync: " + notice)
		}
	}
}

// mergeClone merges ref in the clone. A merge stopping only on graph entries
// upstream deleted while sdd changed them resolves with the deletion and a
// notice naming the deleting commit; any other conflict aborts the merge.
func (c CLI) mergeClone(ctx context.Context, ref, message string) error {
	before, err := c.RevParse(ctx, "HEAD")
	if err != nil {
		return err
	}
	mergeErr := c.runMerging(ctx, "merge", "--no-stat", "--no-edit", "--no-overwrite-ignore", "-m", message, ref)
	if mergeErr != nil {
		resolved, err := c.resolveUpstreamDeletions(ctx, ref)
		if err != nil || !resolved {
			_ = c.run(context.WithoutCancel(ctx), "merge", "--abort")
			return errors.Join(mergeErr, err)
		}
		err = c.runMerging(ctx, "commit", "--no-edit", "--quiet")
		if err != nil {
			_ = c.run(context.WithoutCancel(ctx), "merge", "--abort")
			return err
		}
	}
	after, err := c.RevParse(ctx, "HEAD")
	if err != nil || after == before {
		return err
	}
	merged, err := c.isMergeCommit(ctx, after)
	if err != nil || !merged {
		return err
	}
	return c.RecordGraphCloneCommit(ctx)
}

func (c CLI) isMergeCommit(ctx context.Context, commit string) (bool, error) {
	out, err := c.output(ctx, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return false, err
	}
	return len(strings.Fields(out)) > 2, nil
}

// resolveUpstreamDeletions settles a stopped merge whose every unmerged path
// is a graph entry deleted upstream and changed here: the deletion wins.
func (c CLI) resolveUpstreamDeletions(ctx context.Context, ref string) (bool, error) {
	out, err := c.output(ctx, "-c", "core.quotePath=false", "ls-files", "--unmerged")
	if err != nil {
		return false, err
	}
	stages := map[string][]string{}
	var order []string
	for line := range strings.SplitSeq(out, "\n") {
		meta, path, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 3 {
			continue
		}
		if _, seen := stages[path]; !seen {
			order = append(order, path)
		}
		stages[path] = append(stages[path], fields[2])
	}
	if len(order) == 0 {
		return false, nil
	}
	for _, path := range order {
		if !model.IsUpstreamDeletion(stages[path]) || !model.IsEntryPath(path) {
			return false, nil
		}
	}
	for _, path := range order {
		err = c.run(ctx, "rm", "--quiet", "--", path)
		if err != nil {
			return false, err
		}
		deleting, err := c.output(ctx, "log", "-1", "--format=%H", "--diff-filter=D", ref, "--", path)
		if err != nil {
			return false, err
		}
		slogutils.FromContext(ctx).Info(fmt.Sprintf("sync: %s was deleted upstream in %s; its local title or summary change is dropped", path, deleting))
	}
	return true, nil
}

// predictCloneMerge lists the paths a merge of theirs would leave
// conflicted in the clone: conflicts the drivers settle and entries deleted
// upstream are not among them.
func (c CLI) predictCloneMerge(ctx context.Context, ourRef, theirRef string) ([]string, error) {
	drivers, err := driverArgs()
	if err != nil {
		return nil, err
	}
	args := append(drivers, "-c", "core.quotePath=false", "merge-tree", "--write-tree", "--name-only", "--no-messages", ourRef, theirRef)
	out, err := c.command(ctx, args...).Output()
	if err != nil && errExitCode(err) != 1 {
		return nil, fmt.Errorf("git merge-tree: %w", err)
	}
	conflicts := model.ParseMergeTreeConflicts(string(out))
	if len(conflicts) == 0 {
		return nil, nil
	}
	deleted, err := c.output(ctx, "-c", "core.quotePath=false", "diff", "--name-only", "--diff-filter=D", ourRef+"..."+theirRef)
	if err != nil {
		return nil, err
	}
	deletedUpstream := map[string]bool{}
	for path := range strings.SplitSeq(deleted, "\n") {
		deletedUpstream[path] = true
	}
	var remaining []string
	for _, path := range conflicts {
		if deletedUpstream[path] && model.IsEntryPath(path) {
			continue
		}
		remaining = append(remaining, path)
	}
	return remaining, nil
}

func readOptional(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(data), nil
}

func writeIfChanged(path, content string) error {
	current, err := readOptional(path)
	if err != nil {
		return err
	}
	if current == content {
		return nil
	}
	err = os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

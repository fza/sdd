package model

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
)

// The sdd-owned clone of a graph repository (20261106-202055-d-tac-eyl):
// one partial sparse clone per repository id that only sdd works in.
const (
	// GraphCloneMarker sits at the clone's root and tells a person that hand
	// edits are discarded.
	GraphCloneMarker = "SDD_MANAGED__EDITS_ARE_DISCARDED.md"
	// GraphCloneExcludeLine keeps the marker out of git through the clone's
	// own .git/info/exclude.
	GraphCloneExcludeLine = "/" + GraphCloneMarker
	// GraphCloneCommits names the file in the clone's git directory listing
	// every commit sdd made there, one hash per line.
	GraphCloneCommits = "sdd-commits"
	// ConfigMergeDriver is the merge driver `.sdd/config.yaml` is routed to
	// in the clone.
	ConfigMergeDriver = "sdd-config"
	// GraphElsewhereFile is the stock guard a pointing code repository
	// commits as its graph_dir: stock sdd cannot create a graph below a file.
	GraphElsewhereFile = ".sdd/GRAPH-ELSEWHERE.md"
	// ProtocolSSH / ProtocolHTTPS name the clone's two remotes.
	ProtocolSSH   = "ssh"
	ProtocolHTTPS = "https"
	// HTTPSRetryAfter is how long a clone stays on https before ssh is tried
	// first once more.
	HTTPSRetryAfter = 24 * time.Hour
)

// GraphCloneMarkerText is the marker's content.
const GraphCloneMarkerText = "# Managed by sdd: edits here are discarded\n\n" +
	"sdd keeps this clone of a graph repository for itself. Before every operation it\n" +
	"restores tracked files, removes untracked ones and drops commits it did not make,\n" +
	"without notice.\n\n" +
	"To edit the graph by hand, clone the repository somewhere of your own and work there.\n"

// GraphCloneCone lists the directories the clone checks out: .sdd, and the
// graph directory when it lies outside .sdd. graphDir is relative to the
// repository root, slash-separated.
func GraphCloneCone(graphDir string) []string {
	cone := []string{SDDDirName}
	dir := path.Clean(strings.TrimPrefix(graphDir, "./"))
	if graphDir == "" || dir == SDDDirName || strings.HasPrefix(dir, SDDDirName+"/") || dir == "." {
		return cone
	}
	return append(cone, dir)
}

// GraphRemoteURLs derives the clone's ssh and https remote URLs from the
// repository id.
func GraphRemoteURLs(repoID RepoID) (ssh, https string, err error) {
	err = ValidateRepoID(repoID)
	if err != nil {
		return "", "", err
	}
	host, repoPath, _ := strings.Cut(string(repoID), "/")
	return "git@" + host + ":" + repoPath + ".git", "https://" + host + "/" + repoPath + ".git", nil
}

// InitialProtocol is the protocol a new clone tries first: the code
// repository's origin protocol when that origin is on the graph's host,
// ssh otherwise.
func InitialProtocol(repoID RepoID, codeOrigin string) string {
	originID, err := DeriveRepoID(codeOrigin)
	if err != nil {
		return ProtocolSSH
	}
	graphHost, _, _ := strings.Cut(string(repoID), "/")
	originHost, _, _ := strings.Cut(string(originID), "/")
	if graphHost != originHost {
		return ProtocolSSH
	}
	if strings.HasPrefix(strings.TrimSpace(codeOrigin), "https://") || strings.HasPrefix(strings.TrimSpace(codeOrigin), "http://") {
		return ProtocolHTTPS
	}
	return ProtocolSSH
}

// ProtocolOrder is the order a clone tries its remotes in: the recorded one
// first, the other after a failure; after HTTPSRetryAfter on https, ssh
// first once.
func ProtocolOrder(recorded string, since, now time.Time) []string {
	if recorded == ProtocolHTTPS && !since.IsZero() && now.Sub(since) >= HTTPSRetryAfter {
		return []string{ProtocolSSH, ProtocolHTTPS}
	}
	if recorded == ProtocolHTTPS {
		return []string{ProtocolHTTPS, ProtocolSSH}
	}
	return []string{ProtocolSSH, ProtocolHTTPS}
}

// ConfigMergeAttributeLine routes the committed config to the config merge
// driver.
const ConfigMergeAttributeLine = SDDDirName + "/config.yaml merge=" + ConfigMergeDriver

// RouteMergeAttributes returns the content of a .git/info/attributes that
// keeps every other line and ends with routing, so the later line wins over
// anything before it. Lines routing to sdd's drivers from an earlier
// graph_dir are dropped.
func RouteMergeAttributes(existing string, routing []string) string {
	var kept []string
	for line := range strings.SplitSeq(strings.TrimRight(existing, "\n"), "\n") {
		if line == "" && len(kept) == 0 {
			continue
		}
		if strings.HasSuffix(line, " merge="+EntryMergeDriver) || strings.HasSuffix(line, " merge="+ConfigMergeDriver) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(append(kept, routing...), "\n") + "\n"
}

// EnsureExcludeLine returns exclude content holding line, unchanged when it
// already does.
func EnsureExcludeLine(existing, line string) string {
	if slices.Contains(strings.Split(existing, "\n"), line) {
		return existing
	}
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	return existing + line + "\n"
}

// CloneCommit is one commit ahead of the clone's upstream, oldest first.
type CloneCommit struct {
	Hash  string
	Merge bool
}

// CloneReplay says how to bring the commits ahead of upstream back to the
// ones sdd made: Reset drops them all, and Replay lists sdd's own non-merge
// commits to re-apply on top of upstream, oldest first. Merges sdd made are
// dropped with the rest: upstream already holds what they merged.
type CloneReplay struct {
	Reset  bool
	Replay []string
}

// PlanCloneReplay finds commits ahead that sdd did not record. None found
// leaves the branch as it is.
func PlanCloneReplay(ahead []CloneCommit, recorded []string) CloneReplay {
	known := map[string]bool{}
	for _, hash := range recorded {
		known[hash] = true
	}
	foreign := false
	for _, commit := range ahead {
		if !known[commit.Hash] {
			foreign = true
			break
		}
	}
	if !foreign {
		return CloneReplay{}
	}
	plan := CloneReplay{Reset: true}
	for _, commit := range ahead {
		if known[commit.Hash] && !commit.Merge {
			plan.Replay = append(plan.Replay, commit.Hash)
		}
	}
	return plan
}

// ParseCloneCommits reads the recorded-commits file.
func ParseCloneCommits(content string) []string {
	var hashes []string
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			hashes = append(hashes, line)
		}
	}
	return hashes
}

// GraphElsewhereText is the stock guard's content.
func GraphElsewhereText(repoID RepoID) string {
	return fmt.Sprintf("# This project's graph lives elsewhere\n\n"+
		"The sdd graph of this repository is kept in its own repository, %s.\n\n"+
		"An sdd build that knows `graph_repo` reads and writes it there on its own. With\n"+
		"any other sdd, clone %s and run sdd inside that clone.\n\n"+
		"This file is `graph_dir` here so that an sdd without `graph_repo` support fails\n"+
		"to capture instead of starting a second graph in this repository.\n", repoID, repoID)
}

// IsEntryPath reports whether a repository path names a graph entry file:
// it ends in YYYY/MM/DD-<rest>.md.
func IsEntryPath(repoPath string) bool {
	parts := strings.Split(repoPath, "/")
	if len(parts) < 3 {
		return false
	}
	year, month, file := parts[len(parts)-3], parts[len(parts)-2], parts[len(parts)-1]
	matched, err := path.Match("[0-9][0-9][0-9][0-9]", year)
	if err != nil || !matched {
		return false
	}
	matched, err = path.Match("[0-9][0-9]", month)
	if err != nil || !matched {
		return false
	}
	matched, err = path.Match("[0-9][0-9]-*.md", file)
	return err == nil && matched
}

// IsUpstreamDeletion reports whether the index stages of an unmerged path
// show it deleted on their side and changed on ours: base and ours present,
// theirs missing.
func IsUpstreamDeletion(stages []string) bool {
	return slices.Contains(stages, "1") && slices.Contains(stages, "2") && !slices.Contains(stages, "3")
}

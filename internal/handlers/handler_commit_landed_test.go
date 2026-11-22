package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
)

// landedCommitter commits, then fails to set up the entry merge driver.
type landedCommitter struct{ commits int }

func (c *landedCommitter) Commit(string, ...string) error {
	c.commits++
	return nil
}

func (c *landedCommitter) EnsureMergeDriver(context.Context) error {
	return errors.New("config locked")
}

// landedLock refuses the sync lock once the commit landed, so the publish
// after it fails.
type landedLock struct{ committer *landedCommitter }

func (l *landedLock) TryLock(ctx context.Context) (func(), error) { return l.Lock(ctx) }

func (l *landedLock) Lock(context.Context) (func(), error) {
	if l.committer.commits > 0 {
		return nil, errors.New("lock held")
	}
	return func() {}, nil
}

// A failure after the commit landed names the step that failed and how to
// finish it; it never reads as a failed git commit.
func TestCommitThatLandedIsNotReportedAsAFailedCommit(t *testing.T) {
	text := "A summary set by hand."
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, h *handlers.Handler) error
	}{
		{name: "summarize --all", run: func(_ *testing.T, h *handlers.Handler) error {
			return h.Summarize(context.Background(), &command.SummarizeCmd{})
		}},
		{name: "summarize with --text", run: func(_ *testing.T, h *handlers.Handler) error {
			return h.Summarize(context.Background(), &command.SummarizeCmd{EntryIDs: []string{summarizedEntry}, ExplicitText: &text})
		}},
		{name: "new", run: func(_ *testing.T, h *handlers.Handler) error {
			return h.NewEntry(context.Background(), &command.NewEntryCmd{
				Type: model.TypeSignal, Layer: model.LayerTactical, Description: "A capture whose commit lands.",
				Summary: "The capture commit lands.", SkipPreflight: true,
			})
		}},
		{name: "wip start", run: func(_ *testing.T, h *handlers.Handler) error {
			return h.StartWIP(context.Background(), &command.StartWIPCmd{EntryID: summarizedEntry, Participant: "Ada", Description: "work"})
		}},
		{name: "lint --fix", run: func(_ *testing.T, h *handlers.Handler) error {
			return h.LintFix(context.Background(), &command.LintFixCmd{})
		}},
	} {
		for _, failure := range []struct {
			name    string
			publish bool
			want    string
		}{
			{name: "setup", want: "landed, setting up the entry merge driver failed (sdd init sets it up)"},
			{name: "publish", publish: true, want: "landed, publishing it failed (sdd sync publishes it)"},
		} {
			t.Run(tc.name+"/"+failure.name, func(t *testing.T) {
				graphDir := t.TempDir()
				sddDir := filepath.Join(graphDir, ".sdd")
				if err := os.MkdirAll(sddDir, 0o755); err != nil {
					t.Fatal(err)
				}
				writeSummarizePair(t, graphDir)
				if tc.name == "lint --fix" {
					writeEntryFile(t, graphDir, "20260101-120002-s-prc-ccc", "---\ntype: signal\nlayer: process\nsummary: Lacks a kind.\n---\n\nLacks a kind.\n")
				}
				committer := &landedCommitter{}
				options := handlers.Options{
					GraphDir: graphDir, SDDDir: sddDir, Reader: &graphReader{}, Committer: committer, Stderr: &bytes.Buffer{},
					LLMRunner: &stubRunner{text: "GENERATED"},
				}
				if failure.publish {
					repo := &synctest.Repo{Upstream: "origin/main", Ahead: 1, GraphAhead: 1}
					options.Pusher, options.GraphFetcher, options.GraphPuller = repo, repo, repo
					options.SyncLock, options.SyncRecord = &landedLock{committer: committer}, &synctest.Record{}
					options.SyncMode, options.Gate = model.SyncModeAuto, synctest.Gate(t.TempDir(), nil)
				}
				h := handlers.New(options)

				err := tc.run(t, h)

				if committer.commits != 1 {
					t.Fatalf("the commit should land once, got %d commits", committer.commits)
				}
				if err == nil || !strings.Contains(err.Error(), failure.want) {
					t.Errorf("the error should name the failed step after the landed commit and how to finish it, got %v", err)
				}
				if err != nil && strings.Contains(err.Error(), "git commit:") {
					t.Errorf("a landed commit should not be reported as a failed commit, got %v", err)
				}
			})
		}
	}
}

// Init's commit landed, so a failed merge-driver setup names the setup, not
// the commit.
func TestInitCommitThatLandedIsNotReportedAsAFailedCommit(t *testing.T) {
	repo := t.TempDir()
	committer := &landedCommitter{}
	h := handlers.New(handlers.Options{Reader: finders.New(finders.Options{}), Committer: committer})

	err := h.Init(context.Background(), &command.InitCmd{
		RepoRoot: repo, BinaryVersion: "v0.2.0", Targets: []model.AgentTarget{model.AgentClaude}, Scope: model.ScopeProject,
	})

	if committer.commits != 1 {
		t.Fatalf("the commit should land once, got %d commits", committer.commits)
	}
	if err == nil || !strings.Contains(err.Error(), "landed, setting up the entry merge driver failed (sdd init sets it up)") {
		t.Errorf("the error should name the failed setup after the landed commit, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "git commit") {
		t.Errorf("a landed commit should not be reported as a failed commit, got %v", err)
	}
}

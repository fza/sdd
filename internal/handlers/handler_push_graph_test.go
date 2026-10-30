package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/handlers/synctest"
	"github.com/networkteam/sdd/internal/model"
)

// driverCommitter commits and then fails to set up the entry merge driver,
// noting whether the push had already run when the setup was asked for.
type driverCommitter struct {
	repo           *synctest.Repo
	commits        int
	setupAfterPush bool
	setups         int
}

func (c *driverCommitter) Commit(string, ...string) error {
	c.commits++
	return nil
}

func (c *driverCommitter) EnsureMergeDriver(context.Context) error {
	c.setups++
	c.setupAfterPush = c.repo.Called("push")
	return errors.New("config locked")
}

func TestCommitGraphSetsUpTheMergeDriverAfterThePush(t *testing.T) {
	graphDir := t.TempDir()
	writeSummarizePair(t, graphDir)
	repo := &synctest.Repo{Upstream: "origin/main", Ahead: 1, GraphAhead: 1}
	committer := &driverCommitter{repo: repo}
	h := handlers.New(handlers.Options{
		GraphDir: graphDir, Reader: &graphReader{}, Committer: committer,
		Pusher: repo, GraphFetcher: repo, GraphPuller: repo, SyncLock: &synctest.Lock{}, SyncRecord: &synctest.Record{},
		SyncMode: model.SyncModeAuto,
	})

	err := h.StartWIP(context.Background(), &command.StartWIPCmd{EntryID: summarizedEntry, Participant: "Ada", Description: "work"})

	if err == nil {
		t.Error("a driver setup that fails should be reported")
	}
	if committer.commits != 1 || !repo.Called("push") {
		t.Error("the commit should land and be pushed despite the failing setup")
	}
	if committer.setups != 1 || !committer.setupAfterPush {
		t.Error("the driver setup should run once, after the push")
	}
}

package presenters_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

func TestRepoSyncOutcome(t *testing.T) {
	next := time.Date(2026, 11, 7, 12, 5, 0, 0, time.Local)
	cases := []struct {
		name    string
		outcome model.RepoSyncOutcome
		want    string
	}{
		{name: "synced", outcome: model.RepoSyncOutcome{RepoID: "example.test/a", State: model.RepoSynced}, want: "example.test/a: synced"},
		{name: "pushed", outcome: model.RepoSyncOutcome{RepoID: "example.test/a", State: model.RepoPushed}, want: "example.test/a: pushed"},
		{name: "skipped", outcome: model.RepoSyncOutcome{RepoID: "example.test/a", State: model.RepoSkipped, Next: next}, want: "example.test/a: skipped (next 12:05)"},
		{name: "failed with a retry", outcome: model.RepoSyncOutcome{RepoID: "example.test/a", State: model.RepoFailed, Reason: "remote unreachable", Next: next}, want: "example.test/a: failed (remote unreachable, retry 12:05)"},
		{name: "failed without a retry", outcome: model.RepoSyncOutcome{RepoID: "example.test/a", State: model.RepoFailed, Reason: "remote unreachable"}, want: "example.test/a: failed (remote unreachable)"},
		{name: "refused", outcome: model.RepoSyncOutcome{RepoID: "example.test/a", State: model.RepoRefused, Reason: "not connected"}, want: "example.test/a: refused (not connected)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := presenters.RepoSyncOutcome(tc.outcome)
			if got != tc.want {
				t.Errorf("the outcome should read as `sdd repo sync` prints it, got %q", got)
			}
		})
	}
}

package application_test

import (
	"context"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// Info reports the credential rules the host names, and none without a host
// that names them, rather than process state only the CLI sets.
func TestInfoReportsTheSecretRulesTheHostNames(t *testing.T) {
	fetched := sdd.SecretRules{Source: "fetched", Tag: "v99.0.0", Update: "daily"}
	cases := []struct {
		name  string
		rules func() (sdd.SecretRules, error)
		want  sdd.SecretRules
	}{
		{name: "no host report"},
		{name: "host report", rules: func() (sdd.SecretRules, error) { return fetched, nil }, want: fetched},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := acquiredSnapshot(t, "base", "r1", "Body")
			store := acquiredReadStore{acquire: func(context.Context, sdd.SnapshotReadQuery) (*sdd.AcquiredSnapshot, error) {
				return &sdd.AcquiredSnapshot{Snapshot: snapshot, Config: &sdd.ProjectConfig{}, Attachments: staticGraphStore{}, Release: func() error { return nil }}, nil
			}}
			app, err := sdd.NewApplication(sdd.ApplicationOptions{
				Access: &multiAccessResolver{base: acquiredRuntime(t, "base", store)}, Sessions: noSessionStore{}, StagedBlobs: noBlobStore{},
				SecretRules: tc.rules,
			})
			if err != nil {
				t.Fatal(err)
			}

			info, err := app.Info(t.Context(), sdd.RequestIdentity{Subject: "reader"}, "base", sdd.InfoRequest{})

			if err != nil {
				t.Fatal(err)
			}
			if info.SecretRules != tc.want {
				t.Errorf("info should carry the host's report of the rules in use, got %+v", info.SecretRules)
			}
		})
	}
}

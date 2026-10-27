package application_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

type recordingGraphSync struct {
	branches []string
	err      error
}

func (s *recordingGraphSync) SyncGraph(_ context.Context, branch string) error {
	s.branches = append(s.branches, branch)
	return s.err
}

func TestRefreshWorkflowSyncsTheSessionBranchFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "sync succeeds"},
		{name: "sync fails", err: errors.New("upstream unreachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sync := &recordingGraphSync{err: tc.err}
			graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
				Project: sdd.ProjectRef{ID: "example"}, DefaultBranch: "main", Graph: graph, Sync: sync,
				LLM: pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
					return pkgllm.Result{}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			sessions, err := localadapter.NewFilesystemSessionStoreAt(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: &runtimeAccessResolver{runtime: runtime}, Sessions: sessions, StagedBlobs: blobs})
			if err != nil {
				t.Fatal(err)
			}
			identity := sdd.RequestIdentity{Subject: "christopher"}
			workflow, _, err := application.OpenWorkflow(t.Context(), identity, "example", sdd.WorkflowOpenRequest{ClientName: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := application.RefreshWorkflow(t.Context(), identity, workflow.ID()); err != nil {
				t.Fatalf("a sync failure should not block the session: %v", err)
			}
			if !slices.Equal(sync.branches, []string{"main"}) {
				t.Errorf("a refresh of an unbound session should sync the base branch once, got %v", sync.branches)
			}
		})
	}
}

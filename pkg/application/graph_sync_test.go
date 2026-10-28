package application_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

type recordingGraphSync struct {
	branches []string
	notice   string
	err      error
}

func (s *recordingGraphSync) SyncGraph(_ context.Context, branch string) (string, error) {
	s.branches = append(s.branches, branch)
	return s.notice, s.err
}

func TestRefreshWorkflowSyncsTheSessionBranchFirst(t *testing.T) {
	for _, tc := range []struct {
		name   string
		notice string
		err    error
	}{
		{name: "sync succeeds"},
		{name: "sync raises a notice", notice: "sync: merging origin/main would conflict in a.md."},
		{name: "sync fails", err: errors.New("upstream unreachable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sync := &recordingGraphSync{notice: tc.notice, err: tc.err}
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
			refreshed, err := application.RefreshWorkflow(t.Context(), identity, workflow.ID())
			if err != nil {
				t.Fatalf("a sync failure should not block the session: %v", err)
			}
			if !slices.Equal(sync.branches, []string{"main"}) {
				t.Errorf("a refresh of an unbound session should sync the base branch once, got %v", sync.branches)
			}
			first, err := refreshed.ServeShell(t.Context(), identity)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.HasPrefix(first.Instructions, tc.notice); tc.notice != "" && !got {
				t.Errorf("the first serve after the sync should lead with its notice, got %q", first.Instructions)
			}
			second, err := refreshed.ServeShell(t.Context(), identity)
			if err != nil {
				t.Fatal(err)
			}
			if tc.notice != "" && strings.Contains(second.Instructions, tc.notice) {
				t.Error("the notice should ride one serve only")
			}
		})
	}
}

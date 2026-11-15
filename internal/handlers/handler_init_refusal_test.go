package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

// A graph_dir that would read as more than one .gitignore rule is refused
// before plain init writes anything.
func TestInit_RefusesAGraphDirReadingAsMoreThanOneIgnoreRule(t *testing.T) {
	for _, graphDir := range []string{"!graph", "#graph", "graph\n!secret.txt"} {
		t.Run(graphDir, func(t *testing.T) {
			tmp := t.TempDir()
			h := handlers.New(handlers.Options{Reader: finders.New(finders.Options{})})

			err := h.Init(context.Background(), &command.InitCmd{
				RepoRoot:      tmp,
				GraphDir:      graphDir,
				BinaryVersion: "v0.2.0",
				Scope:         model.ScopeProject,
			})

			if err == nil || !strings.Contains(err.Error(), "reads as more than one path") {
				t.Fatalf("the graph directory should be refused naming the reason, got %v", err)
			}
			entries, readErr := os.ReadDir(tmp)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Errorf("a refused init should write nothing, found %v", entries)
			}
		})
	}
}

type recordingProjectRepositories struct{ inits []string }

func (r *recordingProjectRepositories) RootOf(string) string { return "" }

func (r *recordingProjectRepositories) Init(dir string) error {
	r.inits = append(r.inits, dir)
	return nil
}

func (r *recordingProjectRepositories) CurrentBranch(string) (string, error) { return "main", nil }

func (r *recordingProjectRepositories) RemoteURL(string) string { return "" }

// A library caller passing a project_dir it did not have typed, such as one
// read from a shared config, gets clone advice rather than a new project: the
// handler sets one up only under SetUpProject.
func TestInit_SetsUpAMissingProjectOnlyWhenAsked(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	repos := &recordingProjectRepositories{}
	committer := &recordingCommitter{}
	h := handlers.New(handlers.Options{
		Reader:              finders.New(finders.Options{}),
		ProjectRepositories: repos,
		CommitterAt:         func(string) handlers.Committer { return committer },
	})

	err := h.Init(context.Background(), &command.InitCmd{
		RepoRoot:      work,
		ProjectDir:    "../work-sdd",
		BinaryVersion: "v0.2.0",
		Scope:         model.ScopeProject,
		Language:      "en",
	})

	if err == nil || !strings.Contains(err.Error(), "clone the project there") {
		t.Fatalf("a project_dir naming a missing project should be refused with clone advice, got %v", err)
	}
	if len(repos.inits) != 0 || committer.calls != 0 {
		t.Error("a refused setup should create no repository and commit nothing")
	}
	if _, statErr := os.Stat(filepath.Join(root, "work-sdd")); !os.IsNotExist(statErr) {
		t.Errorf("a refused setup should create no project folder, stat err = %v", statErr)
	}
}

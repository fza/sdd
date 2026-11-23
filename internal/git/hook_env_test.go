package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/git"
)

func seededRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "seed.txt")
	gitIn(t, dir, "commit", "-q", "-m", "seed")
	return dir
}

// hookEnvironments are environments git gives a hook, which sdd started from
// one inherits.
var hookEnvironments = []struct {
	name string
	vars func(hookRepo string) map[string]string
}{
	{name: "relative GIT_DIR", vars: func(string) map[string]string {
		return map[string]string{"GIT_DIR": ".git"}
	}},
	{name: "absolute GIT_DIR", vars: func(hookRepo string) map[string]string {
		return map[string]string{"GIT_DIR": filepath.Join(hookRepo, ".git")}
	}},
	{name: "every repository variable", vars: func(hookRepo string) map[string]string {
		return map[string]string{
			"GIT_DIR":        filepath.Join(hookRepo, ".git"),
			"GIT_WORK_TREE":  hookRepo,
			"GIT_INDEX_FILE": filepath.Join(hookRepo, ".git", "index"),
			"GIT_COMMON_DIR": filepath.Join(hookRepo, ".git"),
		}
	}},
}

// runAsHook sets vars and enters hookRepo, as a hook there starts; the
// returned func unsets vars before the test reads its repositories.
func runAsHook(t *testing.T, hookRepo string, vars map[string]string) func() {
	t.Helper()
	t.Chdir(hookRepo)
	for name, value := range vars {
		t.Setenv(name, value)
	}
	return func() {
		for name := range vars {
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPathAddressedCallsReachTheRepositoryAtThePathFromAHook(t *testing.T) {
	for _, env := range hookEnvironments {
		t.Run(env.name, func(t *testing.T) {
			hookRepo := seededRepo(t)
			graphRepo := seededRepo(t)
			graphDir := filepath.Join(graphRepo, "graph")
			if err := os.MkdirAll(graphDir, 0o755); err != nil {
				t.Fatal(err)
			}
			cache := filepath.Join(t.TempDir(), "cache")
			gitIn(t, t.TempDir(), "clone", "-q", graphRepo, cache)
			gitIn(t, graphRepo, "commit", "-q", "--allow-empty", "-m", "ahead of the cache")
			graphHead := gitIn(t, graphRepo, "rev-parse", "HEAD")
			hookHead := gitIn(t, hookRepo, "rev-parse", "HEAD")
			outside := t.TempDir()
			graphAuthority, err := git.StableRepoRoot(graphRepo)
			if err != nil {
				t.Fatal(err)
			}

			leave := runAsHook(t, hookRepo, env.vars(hookRepo))
			ctx := t.Context()
			head, headErr := git.CLI{}.HeadRevision(ctx, graphDir)
			outsideInWorkTree := git.CLI{}.InWorkTree(ctx, outside)
			pullErr := git.CLI{}.PullFFOnly(ctx, cache)
			authority, authorityErr := git.StableRepoRoot(graphDir)
			sourceErr := git.AuthorizeInTreeSessionSource(ctx, graphRepo, graphAuthority,
				filepath.Join(graphRepo, ".sdd", "sessions"), filepath.Join(graphRepo, ".sdd", "staged-blobs"))
			leave()

			if headErr != nil || head != graphHead {
				t.Errorf("HeadRevision should read the repository at the path: got %q (%v), want %q", head, headErr, graphHead)
			}
			if outsideInWorkTree {
				t.Errorf("InWorkTree should report a directory outside any repository as outside one")
			}
			if pullErr != nil {
				t.Errorf("PullFFOnly should pull the checkout at the path: %v", pullErr)
			}
			if cacheHead := gitIn(t, cache, "rev-parse", "HEAD"); cacheHead != graphHead {
				t.Errorf("the cache at the path should reach %q, HEAD is %q", graphHead, cacheHead)
			}
			if authorityErr != nil || authority != graphAuthority {
				t.Errorf("StableRepoRoot should name the repository at the path: got %q (%v), want %q", authority, authorityErr, graphAuthority)
			}
			if sourceErr != nil {
				t.Errorf("AuthorizeInTreeSessionSource should check the repository at the path: %v", sourceErr)
			}
			if got := gitIn(t, hookRepo, "rev-parse", "HEAD"); got != hookHead {
				t.Errorf("the hook's repository should stay at %q, HEAD is %q", hookHead, got)
			}
		})
	}
}

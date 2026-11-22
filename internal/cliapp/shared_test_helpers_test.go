package cliapp_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/cliapp"
)

func canonicalTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func runApp(t *testing.T, dir string, args ...string) {
	t.Helper()
	t.Chdir(dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg-config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-xdg-data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdg-state"))
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{Version: "test", Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr})
	if err := command.Run(t.Context(), append([]string{"sdd"}, args...)); err != nil {
		t.Fatalf("sdd %s: %v\n%s%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	writeFile(t, filepath.Join(dir, "seed.txt"), "seed\n")
	gitIn(t, dir, "add", "seed.txt")
	gitIn(t, dir, "commit", "-q", "-m", "seed")
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func graphEntries(t *testing.T, graphDir string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(graphDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			rel, _ := filepath.Rel(graphDir, path)
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", graphDir, err)
	}
	return found
}

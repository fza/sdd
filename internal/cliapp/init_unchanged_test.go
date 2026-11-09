package cliapp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

const nothingChanged = "sdd: everything up to date, nothing changed\n"

func setUpInitRepo(t *testing.T) string {
	t.Helper()
	repo := canonicalTemp(t)
	initRepo(t, repo)
	runApp(t, repo, "init", "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada", "--verify-mode", "off")
	return repo
}

func runInit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runAppErr(t, dir, append([]string{"init"}, args...)...)
	if err != nil {
		t.Fatalf("sdd init %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func TestInitThatChangesNothingPrintsOneLine(t *testing.T) {
	repo := setUpInitRepo(t)
	head := headSubject(t, repo)

	if out := runInit(t, repo); out != nothingChanged {
		t.Errorf("a repeat init should print only %q, got:\n%s", nothingChanged, out)
	}
	if headSubject(t, repo) != head {
		t.Error("a repeat init should commit nothing")
	}
}

func TestInitVerboseShowsTheDetailOfAnUnchangedRun(t *testing.T) {
	repo := setUpInitRepo(t)

	out := runInit(t, repo, "--verbose")

	skills := "skills: 2 file(s) at " + filepath.Join(repo, ".claude", "skills") + "\n  up to date\n"
	if !strings.Contains(out, skills) || !strings.HasSuffix(out, nothingChanged) {
		t.Errorf("--verbose should show the per-surface detail, then the summary line, got:\n%s", out)
	}
}

func TestInitThatChangesSomethingKeepsItsReport(t *testing.T) {
	repo := setUpInitRepo(t)

	out := runInit(t, repo, "--participant", "Bo")

	local := filepath.Join(repo, ".sdd", "config.local.yaml")
	if !strings.Contains(out, "  participant: Bo → "+local+"\n") || !strings.Contains(out, "skills: 2 file(s) at ") {
		t.Errorf("an init recording a participant should keep its full report, got:\n%s", out)
	}
	if strings.Contains(out, nothingChanged) {
		t.Errorf("an init that changed something should not claim it changed nothing:\n%s", out)
	}
}

func TestInitSetUpKeepsItsReportInOrder(t *testing.T) {
	repo := canonicalTemp(t)
	initRepo(t, repo)

	out := runInit(t, repo, "--scope", "project", "--agents", "claude", "--language", "en", "--participant", "Ada", "--verify-mode", "off")

	created := strings.Index(out, "created ")
	ignore := strings.Index(out, "  updated "+filepath.Join(repo, ".sdd", ".gitignore"))
	skills := strings.Index(out, "skills: ")
	if created < 0 || ignore < created || skills < ignore || strings.Contains(out, nothingChanged) {
		t.Errorf("a set-up should report what it wrote in the order it wrote it, got:\n%s", out)
	}
}

// A config write reports no surface of its own; the run still counts as a change.
func TestInitThatOnlyWritesConfigIsNoNoOp(t *testing.T) {
	repo := setUpInitRepo(t)

	out := runInit(t, repo, "--verify-mode", "prose")

	if strings.Contains(out, nothingChanged) || !strings.Contains(out, "  up to date\n") {
		t.Errorf("an init recording a setting should keep its report, got:\n%s", out)
	}
}

func TestInitInAPointingRepositoryThatChangesNothingPrintsOneLine(t *testing.T) {
	work, _ := pointedRepos(t)
	runInit(t, work)

	if out := runInit(t, work); out != nothingChanged {
		t.Errorf("a repeat init beside its project should print only %q, got:\n%s", nothingChanged, out)
	}
}

func TestInitWithAGraphRepositoryThatChangesNothingPrintsOneLine(t *testing.T) {
	test.NewGraphRemote(t, graphCloneRepoID)
	code := filepath.Join(canonicalTemp(t), "code")
	err := os.MkdirAll(code, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	initRepo(t, code)
	runInit(t, code, "--graph-repo", "https://example.test/acme/app-sdd.git", "--language", "en", "--scope", "project", "--participant", "Ada", "--agents", "claude", "--verify-mode", "off")

	if out := runInit(t, code); out != nothingChanged {
		t.Errorf("a repeat init with a graph repository should print only %q, got:\n%s", nothingChanged, out)
	}
}

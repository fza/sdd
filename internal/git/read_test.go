package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
)

func commitFile(t *testing.T, dir, name, content, message string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", name)
	gitIn(t, dir, "commit", "-q", "-m", message)
}

// historyRepo holds notes.txt in two versions and other.txt, in three commits.
func historyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	commitFile(t, dir, "notes.txt", "first version\n", "add notes")
	commitFile(t, dir, "notes.txt", "second version\n", "revise notes")
	commitFile(t, dir, "other.txt", "other\n", "add other")
	return dir
}

func readGit(t *testing.T, dir string, request model.GitReadRequest) string {
	t.Helper()
	text, err := git.CLI{Dir: dir}.ReadGit(context.Background(), request)
	if err != nil {
		t.Fatalf("ReadGit: %v", err)
	}
	return text
}

func TestReadGit_ReadsAFileAtARevision(t *testing.T) {
	dir := historyRepo(t)

	older := readGit(t, dir, model.GitReadRequest{Op: model.GitReadFile, Rev: "HEAD~2", Path: "notes.txt"})
	newer := readGit(t, dir, model.GitReadRequest{Op: model.GitReadFile, Rev: "main", Path: "notes.txt"})

	if older != "first version\n" || newer != "second version\n" {
		t.Fatalf("each revision should serve its own content, got %q and %q", older, newer)
	}
}

func TestReadGit_LogsCommitsOfARevisionPathAndRange(t *testing.T) {
	dir := historyRepo(t)

	all := readGit(t, dir, model.GitReadRequest{Op: model.GitReadLog})
	limited := readGit(t, dir, model.GitReadRequest{Op: model.GitReadLog, Limit: 1})
	byPath := readGit(t, dir, model.GitReadRequest{Op: model.GitReadLog, Path: "notes.txt"})
	ranged := readGit(t, dir, model.GitReadRequest{Op: model.GitReadLog, From: "HEAD~1"})

	if strings.Count(all, "\n") != 3 || !strings.Contains(all, "\tadd other") {
		t.Fatalf("the log should list every commit from HEAD, got:\n%s", all)
	}
	if strings.Count(limited, "\n") != 1 || !strings.Contains(limited, "\tadd other") {
		t.Fatalf("the limit should cap the log at the newest commit, got:\n%s", limited)
	}
	if strings.Contains(byPath, "add other") || strings.Count(byPath, "\n") != 2 {
		t.Fatalf("a path should narrow the log to its commits, got:\n%s", byPath)
	}
	if strings.Count(ranged, "\n") != 1 || !strings.Contains(ranged, "\tadd other") {
		t.Fatalf("from should leave out the commits reachable from it, got:\n%s", ranged)
	}
}

func TestReadGit_ShowsACommitAndADiff(t *testing.T) {
	dir := historyRepo(t)

	commit := readGit(t, dir, model.GitReadRequest{Op: model.GitReadCommit, Rev: "HEAD~1"})
	diff := readGit(t, dir, model.GitReadRequest{Op: model.GitReadDiff, From: "HEAD~2", To: "HEAD", Path: "notes.txt"})

	if !strings.Contains(commit, "revise notes") || !strings.Contains(commit, "notes.txt") {
		t.Fatalf("the commit should show its message and changed file, got:\n%s", commit)
	}
	if !strings.Contains(diff, "-first version") || !strings.Contains(diff, "+second version") || strings.Contains(diff, "other.txt") {
		t.Fatalf("the diff should show the path's change between the revisions, got:\n%s", diff)
	}
}

func TestReadGit_RefusesWhatTheRepositoryCannotServe(t *testing.T) {
	dir := historyRepo(t)
	err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{'a', 0, 'b'}, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "blob.bin")
	gitIn(t, dir, "commit", "-q", "-m", "add blob")

	tests := []struct {
		name    string
		request model.GitReadRequest
	}{
		{"revision as an option", model.GitReadRequest{Op: model.GitReadCommit, Rev: "--output=leak"}},
		{"revision without a commit", model.GitReadRequest{Op: model.GitReadCommit, Rev: "no-such-branch"}},
		{"path outside the checkout", model.GitReadRequest{Op: model.GitReadFile, Rev: "HEAD", Path: "../notes.txt"}},
		{"file missing at the revision", model.GitReadRequest{Op: model.GitReadFile, Rev: "HEAD~3", Path: "other.txt"}},
		{"directory as a file", model.GitReadRequest{Op: model.GitReadFile, Rev: "HEAD", Path: "."}},
		{"binary file", model.GitReadRequest{Op: model.GitReadFile, Rev: "HEAD", Path: "blob.bin"}},
		{"unknown operation", model.GitReadRequest{Op: "blame", Rev: "HEAD"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := git.CLI{Dir: dir}.ReadGit(context.Background(), tc.request)

			if !errors.Is(err, model.ErrGitReadRefused) {
				t.Fatalf("the read should be refused for its input, got %v", err)
			}
		})
	}
}

func TestReadGit_TellsWhatIsMissingFromWhatIsRefused(t *testing.T) {
	dir := historyRepo(t)
	err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{'a', 0, 'b'}, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "blob.bin")
	gitIn(t, dir, "commit", "-q", "-m", "add blob")
	read := func(request model.GitReadRequest) error {
		_, err := git.CLI{Dir: dir}.ReadGit(context.Background(), request)
		return err
	}

	missingRev := read(model.GitReadRequest{Op: model.GitReadCommit, Rev: "no-such-branch"})
	missingFile := read(model.GitReadRequest{Op: model.GitReadFile, Rev: "HEAD", Path: "absent.txt"})
	binary := read(model.GitReadRequest{Op: model.GitReadFile, Rev: "HEAD", Path: "blob.bin"})
	noRepo := func() error {
		_, err := git.CLI{Dir: filepath.Join(t.TempDir(), "none")}.ReadGit(context.Background(), model.GitReadRequest{Op: model.GitReadLog})
		return err
	}()

	if !errors.Is(missingRev, model.ErrGitReadMissing) || !errors.Is(missingFile, model.ErrGitReadMissing) {
		t.Fatalf("a revision or file the repository lacks should read as missing, got %v and %v", missingRev, missingFile)
	}
	if !errors.Is(binary, model.ErrGitReadRefused) || errors.Is(binary, model.ErrGitReadMissing) {
		t.Fatalf("a binary file should be refused, not read as missing, got %v", binary)
	}
	if noRepo == nil || errors.Is(noRepo, model.ErrGitReadRefused) {
		t.Fatalf("a checkout git cannot open should fail, not be refused as the caller's input, got %v", noRepo)
	}
}

// A process git leaves behind that holds its output must not keep a read past
// its deadline: the read stops git's whole process group.
func TestReadGit_StopsWhatGitLeftRunningAtItsDeadline(t *testing.T) {
	fake := t.TempDir()
	script := "#!/bin/sh\nsleep 30 &\nsleep 30\n"
	err := os.WriteFile(filepath.Join(fake, "git"), []byte(script), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = git.CLI{Dir: t.TempDir()}.ReadGit(ctx, model.GitReadRequest{Op: model.GitReadLog})
	if err == nil {
		t.Fatalf("a read past its deadline should fail")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the read should end at its deadline, not when what git left running ends, took %s", elapsed)
	}
}

func TestReadGit_RefusesAnAnswerPastItsBound(t *testing.T) {
	dir := historyRepo(t)
	large := strings.Repeat("a line long enough to grow the file quickly\n", 70000)
	commitFile(t, dir, "large.txt", large, "add a large file")

	_, fileErr := git.CLI{Dir: dir}.ReadGit(context.Background(), model.GitReadRequest{Op: model.GitReadFile, Rev: "HEAD", Path: "large.txt"})
	_, diffErr := git.CLI{Dir: dir}.ReadGit(context.Background(), model.GitReadRequest{Op: model.GitReadDiff, From: "HEAD~1", To: "HEAD"})

	if !errors.Is(fileErr, model.ErrGitReadRefused) || !strings.Contains(fileErr.Error(), "more than one read returns") {
		t.Fatalf("a file past the bound should be refused by its size, got %v", fileErr)
	}
	if !errors.Is(diffErr, model.ErrGitReadRefused) || !strings.Contains(diffErr.Error(), "narrow it with a path") {
		t.Fatalf("a diff past the bound should be stopped and refused, got %v", diffErr)
	}
}

// TestReadGit_RunsNoProgramTheRepositoryNames configures an external diff, a
// textconv driver and a pager that each leave a mark when run; no read may run
// one, nor write a file a path shaped like an option names.
func TestReadGit_RunsNoProgramTheRepositoryNames(t *testing.T) {
	dir := historyRepo(t)
	marks := t.TempDir()
	script := filepath.Join(marks, "mark.sh")
	err := os.WriteFile(script, []byte("#!/bin/sh\ntouch \""+marks+"/ran-$1\"\ncat\n"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "config", "diff.external", script+" extdiff")
	gitIn(t, dir, "config", "diff.mark.textconv", script+" textconv")
	gitIn(t, dir, "config", "core.pager", script+" pager")
	commitFile(t, dir, ".gitattributes", "*.txt diff=mark\n", "mark text files")
	commitFile(t, dir, "notes.txt", "third version\n", "revise notes again")

	reads := []model.GitReadRequest{
		{Op: model.GitReadFile, Rev: "HEAD", Path: "notes.txt"},
		{Op: model.GitReadLog, Path: "notes.txt"},
		{Op: model.GitReadCommit, Rev: "HEAD"},
		{Op: model.GitReadDiff, From: "HEAD~1", To: "HEAD", Path: "notes.txt"},
		{Op: model.GitReadDiff, From: "HEAD~1", To: "HEAD", Path: "--output=leak"},
	}
	for _, request := range reads {
		readGit(t, dir, request)
	}

	ran, err := filepath.Glob(filepath.Join(marks, "ran-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ran) > 0 {
		t.Fatalf("no read should run a program the repository names, ran %v", ran)
	}
	_, err = os.Stat(filepath.Join(dir, "leak"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a path shaped like an option should stay a path, not write a file: %v", err)
	}
}

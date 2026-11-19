package cliapp_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/cacheprune"
	"github.com/networkteam/sdd/internal/cliapp"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/secrets"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
	"github.com/networkteam/sdd/test"
)

// runWithPrune runs one command with a recording launcher in place of the
// detached process.
func (f *appFixture) runWithPrune(t *testing.T, launched *[]string, args ...string) appResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{
		Version: appVersion, Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr,
		LaunchCachePrune: func(dir, _ string) error { *launched = append(*launched, dir); return nil },
	})
	err := command.Run(t.Context(), append([]string{"sdd"}, args...))
	return appResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func TestCommandsLaunchTheDailyCachePruneOnce(t *testing.T) {
	f := newAppFixture(t)
	var launched []string
	for range 3 {
		if r := f.runWithPrune(t, &launched, "show", appEntryID); r.err != nil {
			t.Fatalf("show: %v\n%s", r.err, r.stderr)
		}
	}
	if len(launched) != 1 || launched[0] != f.root {
		t.Errorf("three commands should launch one pass from the working directory, launched %v", launched)
	}
}

func TestCachePruneLaunchesNoSecondPass(t *testing.T) {
	f := newAppFixture(t)
	var launched []string
	if r := f.runWithPrune(t, &launched, "cache", "prune"); r.err != nil {
		t.Fatalf("cache prune: %v\n%s", r.err, r.stderr)
	}
	if len(launched) != 0 {
		t.Errorf("sdd cache prune is the pass itself and should launch none, launched %v", launched)
	}
	if r := f.runWithPrune(t, &launched, "show", appEntryID); r.err != nil {
		t.Fatalf("show: %v\n%s", r.err, r.stderr)
	}
	if len(launched) != 0 {
		t.Errorf("a pass run by hand counts as the day's pass, launched %v", launched)
	}
}

func TestCommandsReportAFailedCachePruneOnce(t *testing.T) {
	f := newAppFixture(t)
	stateRoot := filepath.Join(f.root, "state", "sdd")
	err := cacheprune.Update(t.Context(), stateRoot, func(s *model.CachePruneStatus) {
		s.LaunchedAt, s.FinishedAt, s.Error = time.Now(), time.Now(), "removing clone: permission denied"
	})
	if err != nil {
		t.Fatal(err)
	}
	var launched []string
	first := f.runWithPrune(t, &launched, "show", appEntryID)
	second := f.runWithPrune(t, &launched, "show", appEntryID)
	if first.err != nil || second.err != nil {
		t.Fatalf("a failed pass must not fail the command: %v, %v", first.err, second.err)
	}
	if !strings.Contains(first.stderr, "permission denied") {
		t.Errorf("the first command should report the failed pass, stderr:\n%s", first.stderr)
	}
	if strings.Contains(second.stderr, "permission denied") {
		t.Errorf("the failure should be reported once, second stderr:\n%s", second.stderr)
	}
	info := f.run(t, "info", "--format", "json")
	var parsed struct {
		CachePrune struct{ Error string } `json:"cache_prune"`
	}
	if err := json.Unmarshal([]byte(info.stdout), &parsed); err != nil {
		t.Fatalf("info JSON: %v\n%s", err, info.stdout)
	}
	if parsed.CachePrune.Error == "" {
		t.Errorf("sdd info should keep showing the failed pass, got %s", info.stdout)
	}
}

// A scheduled run that fails before it prunes is recorded like any failed
// run, so the next command reports it instead of only the run's log.
func TestScheduledCachePruneRecordsAFailureBeforePruning(t *testing.T) {
	cases := []struct {
		name, broken   string
		outsideProject bool
	}{
		{name: "an unreadable project config, refused before the command runs", broken: ".sdd/config.yaml"},
		{name: "an unreadable user config outside a project, refused before the command runs", broken: "config/sdd/config.yaml", outsideProject: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			f.write(t, tc.broken, "repos: [\n")
			if tc.outsideProject {
				t.Chdir(t.TempDir())
			}
			if r := f.run(t, "cache", "prune", "--scheduled"); r.err == nil {
				t.Fatal("a scheduled run on an unreadable config should fail")
			}
			status, err := cacheprune.Load(filepath.Join(f.root, "state", "sdd"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(status.Error, "config.yaml") || status.FailureNotice() == "" {
				t.Errorf("the failure should be recorded for the next command, got %+v", status)
			}
		})
	}
}

func TestCachePruneCommand(t *testing.T) {
	f := newAppFixture(t)
	clone := filepath.Join(f.root, "cache", "sdd", "github.com", "acme", "dropped", ".git")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(dataHome(f.root), "sdd", "repos", "github.com", "acme", "unused")
	if err := os.MkdirAll(owned, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, owned, "init", "-q", "-b", "main")
	runGit(t, owned, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-q", "--allow-empty", "-m", "sdd: first")
	runGit(t, owned, "remote", "add", "origin", "git@github.com:acme/unused.git")
	runGit(t, owned, "update-ref", "refs/remotes/origin/main", "HEAD")
	undropped := filepath.Join(dataHome(f.root), "sdd", "repos", "github.com", "acme", "graph", ".git")
	if err := os.MkdirAll(undropped, 0o755); err != nil {
		t.Fatal(err)
	}
	f.write(t, "config/sdd/config.yaml", "repos:\n  - repo_id: github.com/acme/kept\n    clone_url: git@github.com:acme/kept.git\n")
	loc, err := repos.DefaultLocations()
	if err != nil {
		t.Fatal(err)
	}
	for _, repoID := range []model.RepoID{"github.com/acme/unused", "github.com/acme/dropped"} {
		if err := repos.NewManager(repos.NewRegistry(loc), nil).MarkDisconnected(repoID); err != nil {
			t.Fatal(err)
		}
	}
	r := f.run(t, "cache", "prune")
	if r.err != nil {
		t.Fatalf("cache prune: %v\n%s", r.err, r.stderr)
	}
	if !strings.Contains(r.stdout, "1 clones") {
		t.Errorf("the run should name what it removed, stdout:\n%s", r.stdout)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Errorf("the sdd-owned clone no connection names and nothing uses should be gone, stat says %v", err)
	}
	if _, err := os.Stat(clone); err != nil {
		t.Errorf("a clone under the cache root should stay, dropped or not, stat says %v", err)
	}
	if _, err := os.Stat(undropped); err != nil {
		t.Errorf("a clone no repo remove dropped should stay, stat says %v", err)
	}
	info := f.run(t, "info")
	if !strings.Contains(info.stdout, "Cache pruning: last run") {
		t.Errorf("sdd info should name the last pass, stdout:\n%s", info.stdout)
	}
}

// Only the sdd binary names GitHub, so no test's daily pass reaches it.
func TestTheDailyPassFetchesNothingWithoutReleaseHosts(t *testing.T) {
	f := newAppFixture(t)

	r := f.run(t, "cache", "prune", "--scheduled")

	if r.err != nil {
		t.Fatalf("cache prune: %v\n%s", r.err, r.stderr)
	}
	state := filepath.Join(f.root, "cache", "sdd", secrets.CacheDirName, "state.json")
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Errorf("the pass should ask no release host, yet it recorded a fetch: stat of %s says %v", state, err)
	}
}

func TestTheDailyPassFetchesFromTheGivenReleaseHosts(t *testing.T) {
	newAppFixture(t)
	releases := test.NewGitleaksReleases(t)
	releases.Publish("v99.0.0", test.NewerGitleaksRules(t))
	var stdout, stderr bytes.Buffer
	command := cliapp.New(cliapp.Options{
		Version: appVersion, Reader: strings.NewReader(""), Writer: &stdout, ErrWriter: &stderr,
		SecretRulesGitHub: secrets.NewGitHub(releases.URL, releases.URL, releases.Client),
	})

	err := command.Run(t.Context(), []string{"sdd", "cache", "prune", "--scheduled"})

	if err != nil {
		t.Fatalf("cache prune: %v\n%s", err, stderr.String())
	}
	if releases.Downloads() != 1 {
		t.Errorf("the pass should fetch the newer release's rule file from the given hosts, asked %v", releases.Requests())
	}
}

func TestConfigSetCacheRetention(t *testing.T) {
	f := newAppFixture(t)
	if r := f.run(t, "config", "set", "cache.retention", "30d"); r.err != nil {
		t.Errorf("a valid retention should be accepted globally: %v\n%s", r.err, r.stderr)
	}
	if r := f.run(t, "config", "set", "cache.retention", "soon"); r.err == nil || !strings.Contains(r.err.Error(), "positive duration") {
		t.Error("an invalid retention should be refused")
	}
	if r := f.run(t, "config", "set", "--local", "cache.retention", "30d"); r.err == nil || !strings.Contains(r.err.Error(), "not a valid per-repo config key") {
		t.Error("a per-repo retention should be refused: the cache is machine-global")
	}
}

func TestConfigSetIndexReplacedRetention(t *testing.T) {
	f := newAppFixture(t)
	if r := f.run(t, "config", "set", "index.replaced_retention", "7d"); r.err != nil {
		t.Errorf("a valid retention should be accepted globally: %v\n%s", r.err, r.stderr)
	}
	if r := f.run(t, "config", "set", "index.replaced_retention", "later"); r.err == nil || !strings.Contains(r.err.Error(), "positive duration") {
		t.Error("an invalid retention should be refused")
	}
	if r := f.run(t, "config", "set", "--local", "index.replaced_retention", "7d"); r.err == nil || !strings.Contains(r.err.Error(), "not a valid per-repo config key") {
		t.Error("a per-repo retention should be refused: the index is machine-global")
	}
}

// Session collection runs in the daily sweep, also with no server running,
// and no longer when sdd serve starts.
func TestTheSweepCollectsSessionsAndServeDoesNot(t *testing.T) {
	f := newAppFixture(t)
	location := localadapter.StoreLocation{
		Name:        "test",
		Sessions:    filepath.Join(f.root, "state", "sdd", "sessions", "example.test", "cli"),
		StagedBlobs: filepath.Join(f.root, "state", "sdd", "staged-blobs", "example.test", "cli"),
		Subject:     "local",
		Project:     "example.test/cli",
	}
	sessions, err := localadapter.NewFilesystemSessionStore(location)
	if err != nil {
		t.Fatal(err)
	}
	ended := time.Now().UTC().Add(-30 * 24 * time.Hour).Round(0)
	metadata := sdd.SessionMetadata{ID: "s_ended", Subject: "local", Project: "example.test/cli", Participant: "Local Author", UpdatedAt: ended}
	created, err := sessions.Create(t.Context(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadata.Ended = &sdd.SessionEnd{Act: sdd.SessionConcluded, EndedAt: ended}
	payload, err := json.Marshal(metadata.Ended)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Append(t.Context(), "s_ended", created.Version, sdd.SessionAppend{Metadata: &metadata, Events: []sdd.StoredEvent{{CodecVersion: sdd.SessionCodecVersion, Code: sdd.SessionEndedEventCode, Payload: payload}}}); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(location.Sessions, "s_ended.jsonl")

	f.serve(t, func(*mcp.ClientSession) {})
	if _, err := os.Stat(log); err != nil {
		t.Fatalf("sdd serve should no longer collect sessions when it starts: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.root, "state", "sdd", "used", "example.test", "cli.stamp")); err != nil {
		t.Errorf("a served project should be stamped as used, so the sweep keeps its state: %v", err)
	}
	r := f.run(t, "cache", "prune")
	if r.err != nil {
		t.Fatalf("cache prune: %v\n%s", r.err, r.stderr)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Errorf("the sweep should remove a session ended past sessions.retention, stat says %v", err)
	}
	if !strings.Contains(r.stdout, "1 sessions") {
		t.Errorf("the run should count the session, stdout:\n%s", r.stdout)
	}
}

// Opening a project records its checkout, so retiring the unused project keeps
// the staged attachments of a session still open in that checkout's
// .sdd/sessions.
func TestTheSweepKeepsTheStagedAttachmentsOfAnOpenInRepoSession(t *testing.T) {
	f := newAppFixture(t)
	inRepo, err := localadapter.NewFilesystemSessionStoreAt(filepath.Join(f.root, ".sdd", "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	metadata := sdd.SessionMetadata{ID: "s_inrepo", Subject: "local", Project: "local", Participant: "Local Author", UpdatedAt: time.Now().UTC().Round(0)}
	if _, err := inRepo.Create(t.Context(), metadata); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(f.root, "state", "sdd", "staged-blobs", "example.test", "cli")
	blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(staged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Stage(t.Context(), sdd.SessionRef{Subject: "local", Session: "s_inrepo"}, "evidence.md", strings.NewReader("evidence")); err != nil {
		t.Fatal(err)
	}

	f.serve(t, func(*mcp.ClientSession) {})
	unused := time.Now().Add(-200 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(f.root, "state", "sdd", "used", "example.test", "cli.stamp"), unused, unused); err != nil {
		t.Fatal(err)
	}
	if r := f.run(t, "cache", "prune"); r.err != nil {
		t.Fatalf("cache prune: %v\n%s", r.err, r.stderr)
	}

	if _, err := os.Stat(filepath.Join(staged, "local", "s_inrepo")); err != nil {
		t.Errorf("the staged attachments of a session open in the checkout should stay: %v", err)
	}
}

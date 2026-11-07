package gitgate_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/gitgate"
)

var t0 = time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock { return &fakeClock{now: t0} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

// fakeRand returns queued values first, then fallback.
type fakeRand struct {
	mu       sync.Mutex
	queue    []float64
	fallback float64
}

func (r *fakeRand) Float64() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.queue) > 0 {
		value := r.queue[0]
		r.queue = r.queue[1:]
		return value
	}
	return r.fallback
}

func defaultLimits() gitgate.Limits {
	return gitgate.Limits{
		FetchCooldown:               5 * time.Minute,
		PushCooldown:                30 * time.Second,
		Retries:                     2,
		Concurrency:                 2,
		SSHPerHour:                  600,
		HTTPSPerHour:                600,
		HTTPSUnauthenticatedPerHour: 60,
	}
}

type gateSetup struct {
	dir    string
	clock  *fakeClock
	rand   *fakeRand
	limits gitgate.Limits
	wait   time.Duration
}

func newSetup(t *testing.T) *gateSetup {
	t.Helper()
	return &gateSetup{dir: t.TempDir(), clock: newClock(), rand: &fakeRand{fallback: 0.5}, limits: defaultLimits(), wait: 100 * time.Millisecond}
}

func (s *gateSetup) gate() *gitgate.Gate {
	return gitgate.New(gitgate.Options{
		StateDir: s.dir,
		Limits:   func(string) gitgate.Limits { return s.limits },
		Now:      s.clock.Now,
		Rand:     s.rand.Float64,
		SlotWait: s.wait,
	})
}

func mustTarget(t *testing.T, rawURL string) gitgate.Target {
	t.Helper()
	target, err := gitgate.TargetFromURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

// writeTrace appends trace2 child_start lines, as git would, to the trace
// file of the admission ctx carries.
func writeTrace(t *testing.T, ctx context.Context, argvs ...[]string) {
	t.Helper()
	env, ok := gitgate.Env(ctx)
	if !ok {
		t.Fatal("op ctx should carry an admission")
	}
	var path string
	for _, entry := range env {
		value, found := strings.CutPrefix(entry, "GIT_TRACE2_EVENT=")
		if found {
			path = value
		}
	}
	if path == "" {
		t.Fatal("admission should name a trace file")
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lines := []any{map[string]any{"event": "version", "evt": "4", "exe": "2.50.1"}}
	for _, argv := range argvs {
		lines = append(lines, map[string]any{"event": "child_start", "child_id": 0, "argv": argv})
	}
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		_, err = file.Write(append(data, '\n'))
		if err != nil {
			t.Fatal(err)
		}
	}
}

var (
	sshChild         = []string{"ssh", "-o", "SendEnv=GIT_PROTOCOL", "git@gitlab.com", "git-upload-pack 'group/project.git'"}
	httpsWrapper     = []string{"git", "remote-https", "origin", "https://gitlab.com/group/project.git"}
	httpsChild       = []string{"git-remote-https", "https://gitlab.com/group/project.git"}
	credentialHelper = []string{"git-credential-osxkeychain", "get"}
)

// sshOp is a successful op that opened one SSH connection.
func sshOp(t *testing.T, runs *int) func(context.Context) error {
	return func(ctx context.Context) error {
		*runs++
		writeTrace(t, ctx, sshChild)
		return nil
	}
}

// failingOp fails with git-like output after opening one SSH connection.
func failingOp(t *testing.T, runs *int, output string) func(context.Context) error {
	return func(ctx context.Context) error {
		*runs++
		writeTrace(t, ctx, sshChild)
		return errors.New("git fetch: exit status 128: " + output)
	}
}

func deferred(t *testing.T, err error) *gitgate.DeferredError {
	t.Helper()
	var target *gitgate.DeferredError
	if !errors.As(err, &target) {
		t.Fatalf("Run should defer, got %v", err)
	}
	return target
}

func bucketCount(t *testing.T, dir, key string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "remotes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ledger struct {
		Version int `json:"version"`
		Buckets map[string]struct {
			Minutes map[string]int `json:"minutes"`
		} `json:"buckets"`
	}
	err = json.Unmarshal(data, &ledger)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range ledger.Buckets[key].Minutes {
		total += n
	}
	return total
}

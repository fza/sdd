package gitgate_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/gitgate"
)

func TestCooldownSkipsWithNext(t *testing.T) {
	setup := newSetup(t)
	gate := setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	runs := 0

	err := gate.Run(t.Context(), target, gitgate.Fetch, sshOp(t, &runs))
	if err != nil {
		t.Fatal(err)
	}

	setup.clock.Set(t0.Add(time.Minute))
	err = gate.Run(t.Context(), target, gitgate.Fetch, sshOp(t, &runs))
	skip := deferred(t, err)
	if skip.Reason != gitgate.ReasonCooldown || !skip.Next.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("fetch within the cooldown should defer until the cooldown ends, got %v", skip)
	}
	if skip.Target != target || skip.Kind != gitgate.Fetch {
		t.Errorf("deferral should name the target and kind, got %v", skip)
	}
	due, next, err := gate.Due(target, gitgate.Fetch)
	if err != nil {
		t.Fatal(err)
	}
	if due || !next.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("Due should report the cooldown end, got %v %v", due, next)
	}
	pushDue, _, err := gate.Due(target, gitgate.Push)
	if err != nil {
		t.Fatal(err)
	}
	if !pushDue {
		t.Error("a fetch should not start the push cooldown")
	}

	setup.clock.Set(t0.Add(5 * time.Minute))
	err = gate.Run(t.Context(), target, gitgate.Fetch, sshOp(t, &runs))
	if err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Errorf("op should run once before and once after the cooldown, ran %d times", runs)
	}
}

func TestPushCooldown(t *testing.T) {
	setup := newSetup(t)
	gate := setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	runs := 0

	err := gate.Run(t.Context(), target, gitgate.Push, sshOp(t, &runs))
	if err != nil {
		t.Fatal(err)
	}
	due, next, err := gate.Due(target, gitgate.Push)
	if err != nil {
		t.Fatal(err)
	}
	if due || !next.Equal(t0.Add(30*time.Second)) {
		t.Errorf("a push should restart the push cooldown, got %v %v", due, next)
	}
}

func TestSuccessJitterBounds(t *testing.T) {
	tests := []struct {
		name string
		rand float64
		next time.Duration
	}{
		{"lowest", 0, 270 * time.Second},
		{"middle", 0.5, 300 * time.Second},
		{"highest", 1, 330 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := newSetup(t)
			setup.rand.fallback = tt.rand
			gate := setup.gate()
			target := mustTarget(t, "git@gitlab.com:group/project.git")
			runs := 0

			err := gate.Run(t.Context(), target, gitgate.Fetch, sshOp(t, &runs))
			if err != nil {
				t.Fatal(err)
			}
			_, next, err := gate.Due(target, gitgate.Fetch)
			if err != nil {
				t.Fatal(err)
			}
			if !next.Equal(t0.Add(tt.next)) {
				t.Errorf("next should move by the jittered cooldown, got %v", next.Sub(t0))
			}
		})
	}
}

func TestFirstFailureRestartsCooldown(t *testing.T) {
	for _, output := range []string{"fatal: Authentication failed for 'https://gitlab.com/group/project.git/'", "fatal: early EOF"} {
		t.Run(output, func(t *testing.T) {
			setup := newSetup(t)
			setup.limits.Retries = 0
			gate := setup.gate()
			target := mustTarget(t, "git@gitlab.com:group/project.git")
			runs := 0

			err := gate.Run(t.Context(), target, gitgate.Fetch, sshOp(t, &runs))
			if err != nil {
				t.Fatal(err)
			}
			failedAt := t0.Add(7 * time.Minute)
			setup.clock.Set(failedAt)
			err = gate.Run(t.Context(), target, gitgate.Fetch, failingOp(t, &runs, output))
			if err == nil {
				t.Fatal("Run should return the op's error")
			}

			setup.clock.Set(failedAt.Add(5*time.Minute - time.Second))
			due, next, err := gate.Due(target, gitgate.Fetch)
			if err != nil {
				t.Fatal(err)
			}
			if due {
				t.Error("fetch should wait after the first failure")
			}
			setup.clock.Set(failedAt.Add(5 * time.Minute))
			due, _, err = gate.Due(target, gitgate.Fetch)
			if err != nil {
				t.Fatal(err)
			}
			if !due {
				t.Errorf("cooldown should restart at the first failure, next %v", next.Sub(failedAt))
			}
		})
	}
}

func TestTransportRetries(t *testing.T) {
	setup := newSetup(t)
	gate := setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	runs := 0
	fail := failingOp(t, &runs, "ssh: Could not resolve hostname gitlab.com: nodename nor servname provided\nfatal: Could not resolve host: gitlab.com")

	err := gate.Run(t.Context(), target, gitgate.Fetch, fail)
	if err == nil {
		t.Fatal("Run should return the op's error")
	}

	// rand 0.5: retry 0.3 of the way to failed_at + cooldown.
	firstRetry := t0.Add(90 * time.Second)
	setup.clock.Set(firstRetry.Add(-time.Second))
	skip := deferred(t, gate.Run(t.Context(), target, gitgate.Fetch, fail))
	if skip.Reason != gitgate.ReasonRetryWait || !skip.Next.Equal(firstRetry) {
		t.Errorf("fetch should wait for the scheduled retry, got %v", skip)
	}

	setup.clock.Set(firstRetry)
	err = gate.Run(t.Context(), target, gitgate.Fetch, fail)
	if errors.As(err, new(*gitgate.DeferredError)) {
		t.Fatalf("a due retry should run, got %v", err)
	}

	secondRetry := firstRetry.Add(63 * time.Second)
	setup.clock.Set(secondRetry.Add(-time.Second))
	skip = deferred(t, gate.Run(t.Context(), target, gitgate.Fetch, fail))
	if skip.Reason != gitgate.ReasonRetryWait || !skip.Next.Equal(secondRetry) {
		t.Errorf("a failed retry should schedule the next within the remaining window, got %v", skip)
	}

	setup.clock.Set(secondRetry)
	err = gate.Run(t.Context(), target, gitgate.Fetch, fail)
	if errors.As(err, new(*gitgate.DeferredError)) {
		t.Fatalf("the second retry should run, got %v", err)
	}

	setup.clock.Set(secondRetry.Add(time.Second))
	skip = deferred(t, gate.Run(t.Context(), target, gitgate.Fetch, fail))
	if skip.Reason != gitgate.ReasonCooldown || !skip.Next.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("used-up retries should wait for the cooldown of the first failure, got %v", skip)
	}
	if runs != 3 {
		t.Errorf("op should run once plus the allowed retries, ran %d times", runs)
	}
}

func TestRetryNeverPastNext(t *testing.T) {
	setup := newSetup(t)
	setup.limits.Retries = 4
	// First failure: jitter 0 puts next at 4m30s; every retry takes the
	// latest moment of its window.
	setup.rand.queue = []float64{0}
	setup.rand.fallback = 1
	gate := setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	runs := 0
	fail := failingOp(t, &runs, "fatal: the remote end hung up unexpectedly")

	at := t0
	for _, retryAt := range []time.Duration{150 * time.Second, 225 * time.Second, 262500 * time.Millisecond} {
		setup.clock.Set(at)
		err := gate.Run(t.Context(), target, gitgate.Fetch, fail)
		if errors.As(err, new(*gitgate.DeferredError)) {
			t.Fatalf("retry at %v should run, got %v", at.Sub(t0), err)
		}
		at = t0.Add(retryAt)
	}
	setup.clock.Set(at)
	err := gate.Run(t.Context(), target, gitgate.Fetch, fail)
	if errors.As(err, new(*gitgate.DeferredError)) {
		t.Fatalf("last retry should run, got %v", err)
	}

	setup.clock.Set(at.Add(time.Second))
	skip := deferred(t, gate.Run(t.Context(), target, gitgate.Fetch, fail))
	if skip.Reason != gitgate.ReasonRetryWait || !skip.Next.Equal(t0.Add(270*time.Second)) {
		t.Errorf("a retry should not be scheduled past next, got %v at %v", skip.Reason, skip.Next.Sub(t0))
	}
}

func TestNoRetryBeforeNext(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{"publickey", "git@gitlab.com: Permission denied (publickey).\nfatal: Could not read from remote repository."},
		{"authentication failed", "remote: HTTP Basic: Access denied\nfatal: Authentication failed for 'https://gitlab.com/group/project.git/'"},
		{"http 401", "fatal: unable to access 'https://gitlab.com/group/project.git/': The requested URL returned error: 401"},
		{"http 403", "fatal: unable to access 'https://gitlab.com/group/project.git/': The requested URL returned error: 403"},
		{"username prompt", "fatal: could not read Username for 'https://gitlab.com': terminal prompts disabled"},
		{"http 429", "error: RPC failed; HTTP 429 curl 22 The requested URL returned error: 429\nfatal: expected flush after ref listing"},
		{"load", "remote: GitLab is unable to handle this request due to load.\nfatal: the remote end hung up unexpectedly"},
		{"rate limit", "remote: Rate limit exceeded\nfatal: the remote end hung up unexpectedly"},
		{"rejected", " ! [rejected]        main -> main (fetch first)\nerror: failed to push some refs"},
		{"non-fast-forward", " ! [rejected]        main -> main (non-fast-forward)"},
		{"unclassified", "fatal: refusing to merge unrelated histories"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := newSetup(t)
			gate := setup.gate()
			target := mustTarget(t, "git@gitlab.com:group/project.git")
			runs := 0

			err := gate.Run(t.Context(), target, gitgate.Fetch, failingOp(t, &runs, tt.output))
			if err == nil {
				t.Fatal("Run should return the op's error")
			}
			setup.clock.Set(t0.Add(4 * time.Minute))
			skip := deferred(t, gate.Run(t.Context(), target, gitgate.Fetch, failingOp(t, &runs, tt.output)))
			if skip.Reason != gitgate.ReasonCooldown || !skip.Next.Equal(t0.Add(5*time.Minute)) {
				t.Errorf("failure should not be retried before next, got %v", skip)
			}
		})
	}
}

func TestTransportFailuresAreRetried(t *testing.T) {
	outputs := []string{
		"ssh: connect to host gitlab.com port 22: Connection timed out",
		"fatal: unable to access 'https://gitlab.com/group/project.git/': Failed to connect to gitlab.com port 443: Timeout was reached",
		"read: Connection reset by peer",
		"ssh: connect to host gitlab.com port 22: Connection refused",
		"fatal: unable to access 'https://gitlab.com/x.git/': Could not resolve host: gitlab.com",
		"fatal: early EOF",
		"fatal: the remote end hung up unexpectedly",
		"error: RPC failed; curl 56 Recv failure",
		"error: RPC failed; HTTP 502 curl 22 The requested URL returned error: 502",
		"fatal: unable to access 'https://gitlab.com/x.git/': The requested URL returned error: 503",
		"ssh_exchange_identification: read: Connection reset by peer",
		"kex_exchange_identification: Connection closed by remote host",
		"Connection closed by remote host",
		"fatal: context deadline exceeded",
	}
	for _, output := range outputs {
		t.Run(output, func(t *testing.T) {
			assertRetried(t, func(ctx context.Context) error {
				writeTrace(t, ctx, sshChild)
				return errors.New(output)
			})
		})
	}
	t.Run("deadline exceeded", func(t *testing.T) {
		assertRetried(t, func(context.Context) error {
			return killedByDeadline{}
		})
	})
}

// killedByDeadline is a git process killed when its ctx ran out: the message
// names only the signal.
type killedByDeadline struct{}

func (killedByDeadline) Error() string { return "git fetch: signal: killed" }

func (killedByDeadline) Unwrap() error { return context.DeadlineExceeded }

func assertRetried(t *testing.T, op func(context.Context) error) {
	t.Helper()
	setup := newSetup(t)
	gate := setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/project.git")

	err := gate.Run(t.Context(), target, gitgate.Fetch, op)
	if err == nil {
		t.Fatal("Run should return the op's error")
	}
	setup.clock.Set(t0.Add(time.Second))
	skip := deferred(t, gate.Run(t.Context(), target, gitgate.Fetch, op))
	if skip.Reason != gitgate.ReasonRetryWait {
		t.Errorf("transport failure should schedule a retry, got %v", skip.Reason)
	}
}

func TestRunReturnsOpError(t *testing.T) {
	setup := newSetup(t)
	gate := setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	sentinel := errors.New("fatal: early EOF")

	err := gate.Run(t.Context(), target, gitgate.Fetch, func(context.Context) error {
		return fmt.Errorf("fetching: %w", sentinel)
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("Run should return the op's error, got %v", err)
	}
}

func TestHourlyLimitPerBucket(t *testing.T) {
	setup := newSetup(t)
	setup.limits.SSHPerHour = 2
	setup.limits.FetchCooldown = time.Minute
	gate := setup.gate()
	runs := 0

	for i := range 2 {
		setup.clock.Set(t0.Add(time.Duration(i) * time.Minute))
		err := gate.Run(t.Context(), mustTarget(t, fmt.Sprintf("git@gitlab.com:group/p%d.git", i)), gitgate.Fetch, sshOp(t, &runs))
		if err != nil {
			t.Fatal(err)
		}
	}

	setup.clock.Set(t0.Add(30 * time.Minute))
	skip := deferred(t, gate.Run(t.Context(), mustTarget(t, "git@gitlab.com:group/p2.git"), gitgate.Fetch, sshOp(t, &runs)))
	if skip.Reason != gitgate.ReasonHourly || !skip.Next.Equal(t0.Add(time.Hour)) {
		t.Errorf("a full bucket should defer until its oldest minute ages out, got %v", skip)
	}

	err := gate.Run(t.Context(), mustTarget(t, "git@github.com:group/p2.git"), gitgate.Fetch, sshOp(t, &runs))
	if err != nil {
		t.Errorf("another host's bucket should have room, got %v", err)
	}
	err = gate.Run(t.Context(), mustTarget(t, "https://gitlab.com/group/p3.git"), gitgate.Fetch, func(ctx context.Context) error {
		writeTrace(t, ctx, httpsChild)
		return nil
	})
	if err != nil {
		t.Errorf("the host's HTTPS buckets should have room, got %v", err)
	}

	setup.clock.Set(t0.Add(time.Hour))
	err = gate.Run(t.Context(), mustTarget(t, "git@gitlab.com:group/p2.git"), gitgate.Fetch, sshOp(t, &runs))
	if err != nil {
		t.Errorf("bucket should have room once the oldest minute aged out, got %v", err)
	}
}

func TestConnectionsCountedFromTrace(t *testing.T) {
	tests := []struct {
		name  string
		argvs [][]string
		ssh   int
		https int
	}{
		{"no git process", nil, 0, 0},
		{"ssh by path", [][]string{{"/usr/bin/ssh", "git@gitlab.com"}}, 1, 0},
		{"ssh command line", [][]string{{"ssh -o IdentitiesOnly=yes", "git@gitlab.com"}}, 1, 0},
		{"https wrapper counted once", [][]string{httpsWrapper, httpsChild}, 0, 1},
		{"http", [][]string{{"/usr/libexec/git-core/git-remote-http", "http://gitlab.com/x"}}, 0, 1},
		{"protocol fallback hits both", [][]string{sshChild, httpsWrapper, httpsChild}, 1, 1},
		{"other children ignored", [][]string{{"git", "index-pack", "--stdin"}, {"git", "rev-list"}}, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup := newSetup(t)
			gate := setup.gate()
			// Known as anonymous after a first op, so HTTPS lands in one bucket.
			target := mustTarget(t, "https://gitlab.com/group/project.git")
			err := gate.Run(t.Context(), target, gitgate.Fetch, func(ctx context.Context) error {
				writeTrace(t, ctx, httpsChild)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			setup.clock.Set(t0.Add(5 * time.Minute))

			err = gate.Run(t.Context(), target, gitgate.Fetch, func(ctx context.Context) error {
				writeTrace(t, ctx, tt.argvs...)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := bucketCount(t, setup.dir, "gitlab.com/ssh"); got != tt.ssh {
				t.Errorf("ssh connections should be counted from the trace, got %d", got)
			}
			if got := bucketCount(t, setup.dir, "gitlab.com/https_unauthenticated") - 1; got != tt.https {
				t.Errorf("https connections should be counted from the trace, got %d", got)
			}
		})
	}
}

func TestUnknownAuthCountsBoth(t *testing.T) {
	t.Run("both buckets need room", func(t *testing.T) {
		setup := newSetup(t)
		setup.limits.HTTPSUnauthenticatedPerHour = 1
		gate := setup.gate()
		err := gate.Run(t.Context(), mustTarget(t, "https://gitlab.com/group/a.git"), gitgate.Fetch, func(ctx context.Context) error {
			writeTrace(t, ctx, httpsWrapper, httpsChild)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		skip := deferred(t, gate.Run(t.Context(), mustTarget(t, "https://gitlab.com/group/b.git"), gitgate.Fetch, func(context.Context) error { return nil }))
		if skip.Reason != gitgate.ReasonHourly {
			t.Errorf("unknown auth should need room in the unauthenticated bucket, got %v", skip.Reason)
		}
	})

	t.Run("reservation hits both", func(t *testing.T) {
		setup := newSetup(t)
		setup.limits.HTTPSPerHour = 1
		setup.limits.HTTPSUnauthenticatedPerHour = 10
		gate := setup.gate()
		err := gate.Run(t.Context(), mustTarget(t, "https://gitlab.com/group/a.git"), gitgate.Fetch, func(ctx context.Context) error {
			due, _, err := setup.gate().Due(mustTarget(t, "https://gitlab.com/group/b.git"), gitgate.Fetch)
			if err != nil {
				return err
			}
			if due {
				t.Error("a running unknown-auth op should reserve the authenticated bucket too")
			}
			if got := bucketCount(t, setup.dir, "gitlab.com/https_unauthenticated"); got != 1 {
				t.Errorf("a running unknown-auth op should reserve the unauthenticated bucket, got %d", got)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("credential helper marks authenticated", func(t *testing.T) {
		setup := newSetup(t)
		setup.limits.HTTPSUnauthenticatedPerHour = 1
		gate := setup.gate()
		target := mustTarget(t, "https://gitlab.com/group/private.git")
		authenticatedOp := func(ctx context.Context) error {
			writeTrace(t, ctx, httpsWrapper, httpsChild, credentialHelper)
			return nil
		}
		err := gate.Run(t.Context(), target, gitgate.Fetch, authenticatedOp)
		if err != nil {
			t.Fatal(err)
		}
		if got := bucketCount(t, setup.dir, "gitlab.com/https"); got != 1 {
			t.Errorf("a login should count in the authenticated bucket, got %d", got)
		}
		if got := bucketCount(t, setup.dir, "gitlab.com/https_unauthenticated"); got != 1 {
			t.Errorf("the anonymous first request should count once, got %d", got)
		}

		setup.clock.Set(t0.Add(5 * time.Minute))
		err = gate.Run(t.Context(), target, gitgate.Fetch, authenticatedOp)
		if err != nil {
			t.Errorf("a known login should not need the full unauthenticated bucket, got %v", err)
		}
		if got := bucketCount(t, setup.dir, "gitlab.com/https_unauthenticated"); got != 1 {
			t.Errorf("a known login should not count against the unauthenticated bucket, got %d", got)
		}
		if got := bucketCount(t, setup.dir, "gitlab.com/https"); got != 2 {
			t.Errorf("a known login should count in the authenticated bucket, got %d", got)
		}
	})
}

func TestNestedRun(t *testing.T) {
	setup := newSetup(t)
	setup.limits.Concurrency = 1
	gate := setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	other := mustTarget(t, "git@gitlab.com:group/other.git")
	runs := 0
	for _, each := range []gitgate.Target{target, other} {
		err := gate.Run(t.Context(), each, gitgate.Fetch, sshOp(t, &runs))
		if err != nil {
			t.Fatal(err)
		}
	}

	setup.clock.Set(t0.Add(time.Minute))
	nestedRan := false
	err := gate.Run(t.Context(), target, gitgate.Push, func(ctx context.Context) error {
		outerEnv, _ := gitgate.Env(ctx)
		err := gate.Run(ctx, target, gitgate.Fetch, func(inner context.Context) error {
			nestedRan = true
			innerEnv, _ := gitgate.Env(inner)
			if strings.Join(innerEnv, " ") != strings.Join(outerEnv, " ") {
				t.Error("nested op should run under the outer admission")
			}
			return nil
		})
		if err != nil {
			return err
		}
		otherErr := gate.Run(ctx, other, gitgate.Fetch, func(context.Context) error { return nil })
		if !errors.As(otherErr, new(*gitgate.DeferredError)) {
			t.Errorf("a nested Run for another target should pass the gate, got %v", otherErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !nestedRan {
		t.Error("nested Run for the same target should run its op despite the fetch cooldown")
	}
}

func TestLocalAlwaysAdmitted(t *testing.T) {
	setup := newSetup(t)
	setup.limits = gitgate.Limits{}
	gate := setup.gate()
	target := mustTarget(t, "/srv/git/project.git")

	for range 3 {
		err := gate.Run(t.Context(), target, gitgate.Fetch, func(ctx context.Context) error {
			env, ok := gitgate.Env(ctx)
			if !ok || len(env) != 0 {
				t.Errorf("a Local op should carry an admission without a trace, got %v %v", env, ok)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("a Local target should always be admitted, got %v", err)
		}
	}
	due, _, err := gate.Due(target, gitgate.Push)
	if err != nil || !due {
		t.Errorf("a Local target should always be due, got %v %v", due, err)
	}
	_, err = os.Stat(filepath.Join(setup.dir, "remotes.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a Local target should be recorded nowhere, got %v", err)
	}
}

func TestEnv(t *testing.T) {
	env, ok := gitgate.Env(t.Context())
	if ok || env != nil {
		t.Errorf("a ctx without an admission should report none, got %v %v", env, ok)
	}

	setup := newSetup(t)
	gate := setup.gate()
	var traceFile string
	err := gate.Run(t.Context(), mustTarget(t, "git@gitlab.com:group/project.git"), gitgate.Fetch, func(ctx context.Context) error {
		env, ok := gitgate.Env(ctx)
		if !ok || len(env) != 1 {
			t.Fatalf("an admitted ctx should carry the trace setting, got %v %v", env, ok)
		}
		traceFile = strings.TrimPrefix(env[0], "GIT_TRACE2_EVENT=")
		if !filepath.IsAbs(traceFile) || !strings.HasPrefix(traceFile, setup.dir) {
			t.Errorf("trace file should be an absolute path under the state dir, got %s", traceFile)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(traceFile)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("trace file should be removed after the outcome is recorded, got %v", err)
	}
}

func TestConcurrencyAcrossGates(t *testing.T) {
	setup := newSetup(t)
	setup.limits.Concurrency = 1
	first, second := setup.gate(), setup.gate()

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- first.Run(context.Background(), mustTarget(t, "git@gitlab.com:group/a.git"), gitgate.Fetch, func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started

	err := second.Run(t.Context(), mustTarget(t, "git@gitlab.com:group/b.git"), gitgate.Fetch, func(context.Context) error { return nil })
	skip := deferred(t, err)
	if skip.Reason != gitgate.ReasonBusy {
		t.Errorf("a host at its concurrency limit should defer as busy, got %v", skip.Reason)
	}
	err = second.Run(t.Context(), mustTarget(t, "git@github.com:group/b.git"), gitgate.Fetch, func(context.Context) error { return nil })
	if err != nil {
		t.Errorf("another host should have a free slot, got %v", err)
	}

	close(release)
	err = <-done
	if err != nil {
		t.Fatal(err)
	}
	err = second.Run(t.Context(), mustTarget(t, "git@gitlab.com:group/b.git"), gitgate.Fetch, func(context.Context) error { return nil })
	if err != nil {
		t.Errorf("a released slot should admit the next op, got %v", err)
	}
}

func TestConcurrencyLimitHolds(t *testing.T) {
	setup := newSetup(t)
	setup.wait = 10 * time.Second
	var running, peak atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := range 6 {
		gate := setup.gate()
		target := mustTarget(t, fmt.Sprintf("git@gitlab.com:group/p%d.git", i))
		wg.Go(func() {
			errs <- gate.Run(context.Background(), target, gitgate.Fetch, func(context.Context) error {
				now := running.Add(1)
				for {
					seen := peak.Load()
					if now <= seen || peak.CompareAndSwap(seen, now) {
						break
					}
				}
				time.Sleep(30 * time.Millisecond)
				running.Add(-1)
				return nil
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("every op should run once a slot frees, got %v", err)
		}
	}
	if peak.Load() > 2 {
		t.Errorf("no more ops than the concurrency limit should run at once, saw %d", peak.Load())
	}
}

func TestOneFetchAcrossProcesses(t *testing.T) {
	setup := newSetup(t)
	setup.wait = 10 * time.Second
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	var runs atomic.Int32
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		gate := setup.gate()
		wg.Go(func() {
			results <- gate.Run(context.Background(), target, gitgate.Fetch, func(context.Context) error {
				runs.Add(1)
				time.Sleep(20 * time.Millisecond)
				return nil
			})
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err == nil {
			continue
		}
		skip := deferred(t, err)
		if skip.Reason != gitgate.ReasonCooldown {
			t.Errorf("a fetch in flight should defer the others by cooldown, got %v", skip.Reason)
		}
	}
	if runs.Load() != 1 {
		t.Errorf("exactly one fetch should run within the cooldown, ran %d", runs.Load())
	}
}

func TestPrecheckDefersWithoutWaitingForSlot(t *testing.T) {
	setup := newSetup(t)
	setup.limits.Concurrency = 1
	first, second := setup.gate(), setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/b.git")
	err := first.Run(t.Context(), target, gitgate.Fetch, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- first.Run(context.Background(), mustTarget(t, "git@gitlab.com:group/a.git"), gitgate.Fetch, func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	skip := deferred(t, second.Run(t.Context(), target, gitgate.Fetch, func(context.Context) error { return nil }))
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if skip.Reason != gitgate.ReasonCooldown {
		t.Errorf("an op in cooldown should be deferred by cooldown, not wait for a slot, got %v", skip.Reason)
	}
}

func TestAdmissionRechecksAfterSlotWait(t *testing.T) {
	setup := newSetup(t)
	setup.limits.Concurrency = 1
	setup.wait = 10 * time.Second
	first, second := setup.gate(), setup.gate()
	target := mustTarget(t, "git@gitlab.com:group/b.git")

	secondRan := false
	secondDone := make(chan error)
	err := first.Run(t.Context(), mustTarget(t, "git@gitlab.com:group/a.git"), gitgate.Fetch, func(context.Context) error {
		go func() {
			secondDone <- second.Run(context.Background(), target, gitgate.Fetch, func(context.Context) error {
				secondRan = true
				return nil
			})
		}()
		time.Sleep(200 * time.Millisecond)
		// Another process fetched b while second waited for the slot.
		fetched := `{"version":1,"repos":{"gitlab.com/group/b":{"protocol":"ssh","fetch":{"outcome":"ok","next":"2026-11-07T10:05:00Z"}}}}`
		return os.WriteFile(filepath.Join(setup.dir, "remotes.json"), []byte(fetched), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	skip := deferred(t, <-secondDone)
	if skip.Reason != gitgate.ReasonCooldown || secondRan {
		t.Errorf("admission should check the cooldown again once it holds a slot, got %v", skip.Reason)
	}
}

func TestLedgerVersion(t *testing.T) {
	t.Run("newer version reads as empty", func(t *testing.T) {
		setup := newSetup(t)
		newer := `{"version":2,"repos":{"gitlab.com/group/project":{"fetch":{"next":"2030-01-01T00:00:00Z"}}}}`
		err := os.WriteFile(filepath.Join(setup.dir, "remotes.json"), []byte(newer), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		runs := 0
		err = setup.gate().Run(t.Context(), mustTarget(t, "git@gitlab.com:group/project.git"), gitgate.Fetch, sshOp(t, &runs))
		if err != nil {
			t.Fatalf("a newer ledger should read as empty, got %v", err)
		}
		data, err := os.ReadFile(filepath.Join(setup.dir, "remotes.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"version":1`) {
			t.Errorf("the ledger should be rewritten at this version, got %s", data)
		}
	})

	t.Run("corrupt ledger fails loud", func(t *testing.T) {
		setup := newSetup(t)
		err := os.WriteFile(filepath.Join(setup.dir, "remotes.json"), []byte("{"), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		ran := false
		err = setup.gate().Run(t.Context(), mustTarget(t, "git@gitlab.com:group/project.git"), gitgate.Fetch, func(context.Context) error {
			ran = true
			return nil
		})
		if err == nil || ran {
			t.Errorf("a corrupt ledger should refuse the op, got %v", err)
		}
	})
}

func TestOldMinutesDropped(t *testing.T) {
	setup := newSetup(t)
	gate := setup.gate()
	runs := 0
	err := gate.Run(t.Context(), mustTarget(t, "git@gitlab.com:group/a.git"), gitgate.Fetch, sshOp(t, &runs))
	if err != nil {
		t.Fatal(err)
	}
	setup.clock.Set(t0.Add(time.Hour))
	err = gate.Run(t.Context(), mustTarget(t, "git@gitlab.com:group/b.git"), gitgate.Fetch, sshOp(t, &runs))
	if err != nil {
		t.Fatal(err)
	}
	if got := bucketCount(t, setup.dir, "gitlab.com/ssh"); got != 1 {
		t.Errorf("minutes older than an hour should be dropped on write, got %d", got)
	}
}

func TestUnreadableTraceFailsLoud(t *testing.T) {
	setup := newSetup(t)
	err := setup.gate().Run(t.Context(), mustTarget(t, "git@gitlab.com:group/a.git"), gitgate.Fetch, func(ctx context.Context) error {
		env, _ := gitgate.Env(ctx)
		return os.WriteFile(strings.TrimPrefix(env[0], "GIT_TRACE2_EVENT="), []byte("not json\n"), 0o600)
	})
	if err == nil {
		t.Error("an unreadable trace should surface as an error")
	}
	if got := bucketCount(t, setup.dir, "gitlab.com/ssh"); got != 1 {
		t.Errorf("an unreadable trace should keep the reservation, got %d", got)
	}
}

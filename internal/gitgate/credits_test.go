package gitgate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/gitgate"
)

// runAt runs a successful SSH op of kind at at.
func runAt(t *testing.T, setup *gateSetup, ctx context.Context, target gitgate.Target, kind gitgate.Kind, at time.Time) error {
	t.Helper()
	setup.clock.Set(at)
	runs := 0
	return setup.gate().Run(ctx, target, kind, sshOp(t, &runs))
}

func mustRun(t *testing.T, setup *gateSetup, ctx context.Context, target gitgate.Target, kind gitgate.Kind, at time.Time) {
	t.Helper()
	err := runAt(t, setup, ctx, target, kind, at)
	if err != nil {
		t.Fatalf("%s at %v should run, got %v", kind, at.Sub(t0), err)
	}
}

// paidPushes pushes target at from without a credit, then every 15 seconds
// with one, and counts the pushes admitted before the first deferral.
func paidPushes(t *testing.T, setup *gateSetup, target gitgate.Target, from time.Time) (int, *gitgate.DeferredError) {
	t.Helper()
	mustRun(t, setup, t.Context(), target, gitgate.Push, from)
	credited := gitgate.WithCredits(t.Context(), gitgate.Push)
	for paid := range 10 {
		err := runAt(t, setup, credited, target, gitgate.Push, from.Add(time.Duration(paid+1)*15*time.Second))
		if err != nil {
			return paid, deferred(t, err)
		}
	}
	t.Fatal("pushes should run out of credits")
	return 0, nil
}

// The fetch cooldown stretches to the slice that keeps one fetch per tracked
// repository per slice within the bucket's hourly limit.
func TestFetchSliceStretchesCooldown(t *testing.T) {
	anonymous := func(t *testing.T) func(context.Context) error {
		return func(ctx context.Context) error {
			writeTrace(t, ctx, httpsChild)
			return nil
		}
	}
	authenticated := func(t *testing.T) func(context.Context) error {
		return func(ctx context.Context) error {
			writeTrace(t, ctx, httpsChild, credentialHelper)
			return nil
		}
	}
	unknown := func(*testing.T) func(context.Context) error {
		return func(context.Context) error { return nil }
	}
	cases := []struct {
		name   string
		limits func(*gitgate.Limits)
		url    string
		op     func(*testing.T) func(context.Context) error
		repos  int
		want   time.Duration
	}{
		{name: "ssh repositories past the bucket's share", limits: func(l *gitgate.Limits) { l.SSHPerHour = 10 }, url: "git@gitlab.com:group/p%d.git", op: sshOpOf, repos: 3, want: 20 * time.Minute},
		{name: "the configured cooldown when it is longer", url: "git@gitlab.com:group/p%d.git", op: sshOpOf, repos: 3, want: 5 * time.Minute},
		{name: "known anonymous HTTPS uses its bucket", limits: func(l *gitgate.Limits) { l.HTTPSUnauthenticatedPerHour = 10 }, url: "https://gitlab.com/group/p%d.git", op: anonymous, repos: 2, want: 800 * time.Second},
		{name: "known login uses the authenticated bucket", limits: func(l *gitgate.Limits) { l.HTTPSUnauthenticatedPerHour = 10 }, url: "https://gitlab.com/group/p%d.git", op: authenticated, repos: 2, want: 5 * time.Minute},
		{name: "unknown login uses the smaller limit", limits: func(l *gitgate.Limits) { l.HTTPSUnauthenticatedPerHour = 10 }, url: "https://gitlab.com/group/p%d.git", op: unknown, repos: 2, want: 800 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newSetup(t)
			if tc.limits != nil {
				tc.limits(&setup.limits)
			}
			gate := setup.gate()
			var last gitgate.Target
			for i := range tc.repos {
				last = setup.clone(t, fmt.Sprintf(tc.url, i))
				err := gate.Run(t.Context(), last, gitgate.Fetch, tc.op(t))
				if err != nil {
					t.Fatal(err)
				}
			}
			_, next, err := gate.Due(last, gitgate.Fetch)
			if err != nil {
				t.Fatal(err)
			}
			if !next.Equal(t0.Add(tc.want)) {
				t.Errorf("the fetch cooldown should be the repositories' slice of the hourly limit, got %v", next.Sub(t0))
			}
		})
	}
}

func sshOpOf(t *testing.T) func(context.Context) error {
	runs := 0
	return sshOp(t, &runs)
}

// Each fetch slice a repository leaves unused earns one credit, and a push
// within its cooldown spends one; an empty wallet defers as the cooldown does.
func TestCreditsFromIdleSlices(t *testing.T) {
	cases := []struct {
		name string
		idle time.Duration
		paid int
	}{
		{name: "slice in progress", idle: 9 * time.Minute},
		{name: "one slice unused", idle: 10 * time.Minute, paid: 1},
		{name: "three slices unused", idle: 20*time.Minute + time.Second, paid: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newSetup(t)
			target := setup.clone(t, "git@gitlab.com:group/project.git")
			mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0)

			paid, skip := paidPushes(t, setup, target, t0.Add(tc.idle))
			if paid != tc.paid {
				t.Errorf("each unused slice should pay for one push, got %d", paid)
			}
			lastPush := t0.Add(tc.idle + time.Duration(paid)*15*time.Second)
			if skip.Reason != gitgate.ReasonCooldown || !skip.Next.Equal(lastPush.Add(30*time.Second)) {
				t.Errorf("an empty wallet should defer until the push cooldown ends, got %v", skip)
			}
		})
	}
}

// Only an operation its caller marks may spend a credit, and only of the kind
// it marks.
func TestCreditSpendNeedsTheMark(t *testing.T) {
	target := mustTarget(t, "git@gitlab.com:group/project.git")
	idle := t0.Add(20*time.Minute + time.Second)
	cases := []struct {
		name string
		ctx  func(context.Context) context.Context
		kind gitgate.Kind
		paid bool
	}{
		{name: "unmarked push", ctx: func(ctx context.Context) context.Context { return ctx }, kind: gitgate.Push},
		{name: "push marked for fetches", ctx: func(ctx context.Context) context.Context { return gitgate.WithCredits(ctx, gitgate.Fetch) }, kind: gitgate.Push},
		{name: "marked push", ctx: func(ctx context.Context) context.Context { return gitgate.WithCredits(ctx, gitgate.Push) }, kind: gitgate.Push, paid: true},
		{name: "unmarked fetch", ctx: func(ctx context.Context) context.Context { return ctx }, kind: gitgate.Fetch},
		{name: "marked fetch", ctx: func(ctx context.Context) context.Context {
			return gitgate.WithCredits(ctx, gitgate.Fetch, gitgate.Push)
		}, kind: gitgate.Fetch, paid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newSetup(t)
			setup.clones = []gitgate.Target{target}
			mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0)
			mustRun(t, setup, t.Context(), target, tc.kind, idle)

			// Past the floor, inside the cooldown.
			after := 15 * time.Second
			if tc.kind == gitgate.Fetch {
				after = 90 * time.Second
			}
			err := runAt(t, setup, tc.ctx(t.Context()), target, tc.kind, idle.Add(after))
			if tc.paid && err != nil {
				t.Errorf("a marked operation should spend a credit, got %v", err)
			}
			if !tc.paid && deferred(t, err).Reason != gitgate.ReasonCooldown {
				t.Errorf("an operation not marked should keep its cooldown, got %v", err)
			}
		})
	}
}

// A credit never runs a repository's fetches closer than a minute apart, or
// its pushes closer than ten seconds.
func TestCreditFloors(t *testing.T) {
	cases := []struct {
		name  string
		kind  gitgate.Kind
		floor time.Duration
	}{
		{name: "fetch", kind: gitgate.Fetch, floor: time.Minute},
		{name: "push", kind: gitgate.Push, floor: 10 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newSetup(t)
			target := setup.clone(t, "git@gitlab.com:group/project.git")
			credited := gitgate.WithCredits(t.Context(), tc.kind)
			mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0)
			last := t0.Add(20*time.Minute + time.Second)
			mustRun(t, setup, t.Context(), target, tc.kind, last)

			skip := deferred(t, runAt(t, setup, credited, target, tc.kind, last.Add(tc.floor-time.Second)))
			if skip.Reason != gitgate.ReasonCooldown || !skip.Next.Equal(last.Add(tc.floor)) {
				t.Errorf("a credit should wait out the floor, got %v", skip)
			}
			mustRun(t, setup, credited, target, tc.kind, last.Add(tc.floor))
		})
	}
}

// A full hourly bucket defers an operation even with a credit to pay for it.
func TestCreditsKeepTheHourlyLimit(t *testing.T) {
	setup := newSetup(t)
	setup.limits.SSHPerHour = 4
	target := setup.clone(t, "git@gitlab.com:group/a.git")
	mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0)
	full := t0.Add(40 * time.Minute)
	mustRun(t, setup, t.Context(), target, gitgate.Push, full)
	mustRun(t, setup, t.Context(), mustTarget(t, "git@gitlab.com:group/b.git"), gitgate.Push, full)
	mustRun(t, setup, t.Context(), mustTarget(t, "git@gitlab.com:group/c.git"), gitgate.Push, full)

	skip := deferred(t, runAt(t, setup, gitgate.WithCredits(t.Context(), gitgate.Push), target, gitgate.Push, full.Add(15*time.Second)))
	if skip.Reason != gitgate.ReasonHourly || !skip.Next.Equal(t0.Add(time.Hour)) {
		t.Errorf("a full bucket should defer a credited push until its oldest minute ages out, got %v", skip)
	}
}

// A credit ages out of the wallet an hour after the slice that earned it came
// due.
func TestCreditsAgeOut(t *testing.T) {
	cases := []struct {
		name string
		at   time.Duration
		paid int
	}{
		{name: "within the hour", at: 74 * time.Minute, paid: 1},
		{name: "past the hour", at: 75 * time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newSetup(t)
			setup.limits.FetchCooldown = 15 * time.Minute
			target := setup.clone(t, "git@gitlab.com:group/project.git")
			// The slice due at 15m goes unused; every later one is fetched.
			for _, at := range []time.Duration{0, 30 * time.Minute, 45 * time.Minute, 60 * time.Minute} {
				mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0.Add(at))
			}

			paid, _ := paidPushes(t, setup, target, t0.Add(tc.at))
			if paid != tc.paid {
				t.Errorf("a credit should count for the hour after its slice came due, paid %d", paid)
			}
		})
	}
}

// After a failed operation its cooldown holds; a credit does not pay past it.
func TestNoCreditAfterAFailure(t *testing.T) {
	setup := newSetup(t)
	target := setup.clone(t, "git@gitlab.com:group/project.git")
	mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0)
	failedAt := t0.Add(20*time.Minute + time.Second)
	setup.clock.Set(failedAt)
	runs := 0
	err := setup.gate().Run(t.Context(), target, gitgate.Push, failingOp(t, &runs, "! [rejected] main -> main (fetch first)"))
	if err == nil {
		t.Fatal("the push should fail")
	}

	skip := deferred(t, runAt(t, setup, gitgate.WithCredits(t.Context(), gitgate.Push), target, gitgate.Push, failedAt.Add(15*time.Second)))
	if skip.Reason != gitgate.ReasonCooldown || !skip.Next.Equal(failedAt.Add(30*time.Second)) {
		t.Errorf("a failed push should keep its cooldown, got %v", skip)
	}
}

// A ledger written before credits existed earns them from each repository's
// next allowed fetch.
func TestLedgerWithoutCredits(t *testing.T) {
	setup := newSetup(t)
	earlier := `{"version":1,"buckets":{},"repos":{"gitlab.com/group/project":{"protocol":"ssh","fetch":{"last_attempt":"2026-11-07T09:35:00Z","outcome":"ok","retries_used":0,"next":"2026-11-07T09:40:00Z"}}}}`
	err := os.WriteFile(filepath.Join(setup.dir, "remotes.json"), []byte(earlier), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	paid, _ := paidPushes(t, setup, setup.clone(t, "git@gitlab.com:group/project.git"), t0.Add(time.Second))
	if paid != 4 {
		t.Errorf("the slices since the recorded next fetch should earn credits, paid %d", paid)
	}
}

// Only the sdd-owned clones present share the fetch slices and earn credits:
// a code repository the gate also fetches neither stretches the slice nor
// earns a credit.
func TestOnlyClonesShareSlicesAndEarnCredits(t *testing.T) {
	setup := newSetup(t)
	setup.limits.SSHPerHour = 10
	clone := setup.clone(t, "git@gitlab.com:group/clone.git")
	code := mustTarget(t, "git@gitlab.com:group/code.git")
	for _, target := range []gitgate.Target{mustTarget(t, "git@gitlab.com:group/removed.git"), code, clone} {
		mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0)
	}
	_, next, err := setup.gate().Due(clone, gitgate.Fetch)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Equal(t0.Add(400 * time.Second)) {
		t.Errorf("the slice should count the clones alone, got %v", next.Sub(t0))
	}

	paid, _ := paidPushes(t, setup, code, t0.Add(20*time.Minute))
	if paid != 2 {
		t.Errorf("only the clone's unused slices should earn credits, paid %d", paid)
	}
}

// Automatic fetches stop at nine tenths of the hourly limit; pushes and
// commanded syncs may use the rest, up to the limit.
func TestAutomaticFetchesKeepAReserve(t *testing.T) {
	setup := newSetup(t)
	setup.limits.SSHPerHour = 10
	setup.limits.SSHPerMinute = 100
	for i := range 9 {
		mustRun(t, setup, t.Context(), mustTarget(t, fmt.Sprintf("git@gitlab.com:group/p%d.git", i)), gitgate.Fetch, t0)
	}

	skip := deferred(t, runAt(t, setup, t.Context(), mustTarget(t, "git@gitlab.com:group/auto.git"), gitgate.Fetch, t0))
	if skip.Reason != gitgate.ReasonHourly || !skip.Next.Equal(t0.Add(time.Hour)) {
		t.Errorf("an automatic fetch should stop at the fetch share, got %v", skip)
	}
	commanded := gitgate.WithCredits(t.Context(), gitgate.Fetch)
	mustRun(t, setup, commanded, mustTarget(t, "git@gitlab.com:group/commanded.git"), gitgate.Fetch, t0)
	skip = deferred(t, runAt(t, setup, t.Context(), mustTarget(t, "git@gitlab.com:group/p0.git"), gitgate.Push, t0))
	if skip.Reason != gitgate.ReasonHourly {
		t.Errorf("the reserve should end at the hourly limit, got %v", skip)
	}
}

func TestPushUsesTheReserve(t *testing.T) {
	setup := newSetup(t)
	setup.limits.SSHPerHour = 10
	setup.limits.SSHPerMinute = 100
	for i := range 9 {
		mustRun(t, setup, t.Context(), mustTarget(t, fmt.Sprintf("git@gitlab.com:group/p%d.git", i)), gitgate.Fetch, t0)
	}
	mustRun(t, setup, t.Context(), mustTarget(t, "git@gitlab.com:group/p0.git"), gitgate.Push, t0)
}

// Once a bucket's operations in the current minute reach its per-minute
// limit, every operation waits for the next minute, credit or not.
func TestPerMinuteLimit(t *testing.T) {
	setup := newSetup(t)
	setup.limits.SSHPerMinute = 2
	target := setup.clone(t, "git@gitlab.com:group/project.git")
	mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0)
	busy := t0.Add(20*time.Minute + 10*time.Second)
	mustRun(t, setup, t.Context(), target, gitgate.Push, busy)
	mustRun(t, setup, t.Context(), mustTarget(t, "git@gitlab.com:group/other.git"), gitgate.Push, busy)

	credited := gitgate.WithCredits(t.Context(), gitgate.Push)
	skip := deferred(t, runAt(t, setup, credited, target, gitgate.Push, busy.Add(20*time.Second)))
	if skip.Reason != gitgate.ReasonMinute || !skip.Next.Equal(t0.Add(21*time.Minute)) {
		t.Errorf("a full minute should defer even a credited push to the next minute, got %v", skip)
	}
	mustRun(t, setup, credited, target, gitgate.Push, t0.Add(21*time.Minute))
}

// A clone counts in the bucket its login puts it in: an HTTPS clone known to
// log in does not stretch the slices of the clones without login.
func TestClonesCountPerBucket(t *testing.T) {
	setup := newSetup(t)
	setup.limits.HTTPSUnauthenticatedPerHour = 10
	gate := setup.gate()
	err := gate.Run(t.Context(), setup.clone(t, "https://gitlab.com/group/private.git"), gitgate.Fetch, func(ctx context.Context) error {
		writeTrace(t, ctx, httpsWrapper, httpsChild, credentialHelper)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	public := setup.clone(t, "https://gitlab.com/group/public.git")
	err = gate.Run(t.Context(), public, gitgate.Fetch, func(ctx context.Context) error {
		writeTrace(t, ctx, httpsChild)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, next, err := gate.Due(public, gitgate.Fetch)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Equal(t0.Add(400 * time.Second)) {
		t.Errorf("only the clones without login should share the unauthenticated slice, got %v", next.Sub(t0))
	}
}

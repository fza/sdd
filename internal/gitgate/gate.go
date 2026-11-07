package gitgate

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	defaultSlotWait = 30 * time.Second
	slotRetryDelay  = 20 * time.Millisecond
	jitterSpread    = 0.1
	retryMinShare   = 0.1
	retryMaxShare   = 0.5
)

type Gate struct {
	stateDir string
	store    ledgerStore
	limits   LimitsFunc
	now      func() time.Time
	rand     func() float64
	slotWait time.Duration
}

type Options struct {
	// StateDir holds remotes.json, its lock, the git-slots and traces
	// directories.
	StateDir string
	Limits   LimitsFunc
	// Now defaults to time.Now.
	Now func() time.Time
	// Rand returns a uniform value in [0, 1); defaults to math/rand/v2.
	Rand func() float64
	// SlotWait is how long Run waits for a concurrency slot; 0 means 30s.
	SlotWait time.Duration
}

func New(opts Options) *Gate {
	gate := &Gate{
		stateDir: opts.StateDir,
		store:    ledgerStore{dir: opts.StateDir},
		limits:   opts.Limits,
		now:      opts.Now,
		rand:     opts.Rand,
		slotWait: opts.SlotWait,
	}
	if gate.now == nil {
		gate.now = time.Now
	}
	if gate.rand == nil {
		gate.rand = rand.Float64
	}
	if gate.slotWait <= 0 {
		gate.slotWait = defaultSlotWait
	}
	return gate
}

type verdict struct {
	due      bool
	viaRetry bool
	reason   Reason
	next     time.Time
	buckets  []string
}

type reservation struct {
	minute   int64
	buckets  []string
	viaRetry bool
}

// Due reports whether kind is due for target now, and when it next is. It
// does not look at concurrency slots.
func (g *Gate) Due(target Target, kind Kind) (due bool, next time.Time, err error) {
	now := g.now()
	if target.Protocol == Local {
		return true, now, nil
	}
	limits, err := g.limitsFor(target)
	if err != nil {
		return false, time.Time{}, err
	}
	v, err := g.check(context.Background(), target, kind, limits, now)
	if err != nil {
		return false, time.Time{}, err
	}
	return v.due, v.next, nil
}

// Run runs op when the gate admits it, else returns *DeferredError without
// running op. While op runs, ctx carries the admission Env reads. A nested Run
// for the same target on an admitted ctx runs op directly: a push's fetch and
// merge belong to the push. op's error is returned as is, joined with any
// failure to record the outcome.
func (g *Gate) Run(ctx context.Context, target Target, kind Kind, op func(ctx context.Context) error) error {
	if target.Protocol == Local {
		return op(withAdmission(ctx, &admission{target: target}))
	}
	current, admitted := admissionFrom(ctx)
	if admitted && current.target == target {
		return op(ctx)
	}
	limits, err := g.limitsFor(target)
	if err != nil {
		return err
	}

	// Checked before waiting for a slot, so an operation in cooldown is not
	// held up to SlotWait only to be refused.
	precheck, err := g.check(ctx, target, kind, limits, g.now())
	if err != nil {
		return err
	}
	if !precheck.due {
		return g.deferred(target, kind, precheck)
	}

	release, err := g.acquireSlot(ctx, target, kind, limits)
	if err != nil {
		return err
	}
	defer release()

	traceFile, err := g.newTraceFile()
	if err != nil {
		return err
	}
	res, v, err := g.admit(ctx, target, kind, limits)
	if err != nil || !v.due {
		removeErr := os.Remove(traceFile)
		if err != nil {
			return errors.Join(err, removeErr)
		}
		return errors.Join(g.deferred(target, kind, v), removeErr)
	}

	opErr := op(withAdmission(ctx, &admission{target: target, traceFile: traceFile}))

	counts, traceErr := readTrace(traceFile)
	removeErr := os.Remove(traceFile)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		traceErr = errors.Join(traceErr, fmt.Errorf("removing git trace: %w", removeErr))
	}
	recordErr := g.record(context.WithoutCancel(ctx), target, kind, limits, res, counts, traceErr == nil, opErr)
	if traceErr == nil && recordErr == nil {
		return opErr
	}
	return errors.Join(opErr, traceErr, recordErr)
}

// Concurrency is how many operations may run against target's host at once,
// at least one.
func (g *Gate) Concurrency(target Target) (int, error) {
	limits, err := g.limitsFor(target)
	if err != nil {
		return 0, err
	}
	return max(limits.Concurrency, 1), nil
}

func (g *Gate) limitsFor(target Target) (Limits, error) {
	if g.limits == nil {
		return Limits{}, errors.New("git gate has no limits")
	}
	return g.limits(target.Host), nil
}

func (g *Gate) deferred(target Target, kind Kind, v verdict) error {
	return &DeferredError{Target: target, Kind: kind, Reason: v.reason, Next: v.next}
}

func (g *Gate) check(ctx context.Context, target Target, kind Kind, limits Limits, now time.Time) (verdict, error) {
	unlock, err := g.store.lock(ctx, true)
	if err != nil {
		return verdict{}, err
	}
	defer unlock()
	l, err := g.store.read()
	if err != nil {
		return verdict{}, err
	}
	return evaluate(l, target, kind, limits, now), nil
}

func (g *Gate) admit(ctx context.Context, target Target, kind Kind, limits Limits) (reservation, verdict, error) {
	unlock, err := g.store.lock(ctx, false)
	if err != nil {
		return reservation{}, verdict{}, err
	}
	defer unlock()
	l, err := g.store.read()
	if err != nil {
		return reservation{}, verdict{}, err
	}
	now := g.now()
	v := evaluate(l, target, kind, limits, now)
	if !v.due {
		return reservation{}, v, nil
	}

	res := reservation{minute: unixMinute(now), buckets: v.buckets, viaRetry: v.viaRetry}
	for _, key := range res.buckets {
		l.add(key, res.minute, 1)
	}
	repo := l.repo(target)
	repo.Protocol = target.Protocol
	record := repo.kind(kind)
	record.LastAttempt = now.UTC()
	record.RetryAt = time.Time{}
	// Holds off other processes while op runs; record replaces it.
	if !res.viaRetry {
		record.Next = now.Add(cooldown(limits, kind)).UTC()
	}
	err = g.store.write(l, now)
	if err != nil {
		return reservation{}, verdict{}, err
	}
	return res, v, nil
}

func evaluate(l *ledger, target Target, kind Kind, limits Limits, now time.Time) verdict {
	repo := l.Repos[target.Repo]
	if repo == nil {
		repo = &repoRecord{}
	}
	v := verdict{due: true, next: now}

	var record *kindRecord
	if kind == Push {
		record = repo.Push
	} else {
		record = repo.Fetch
	}
	if record != nil && now.Before(record.Next) {
		switch {
		case !record.RetryAt.IsZero() && !now.Before(record.RetryAt):
			v.viaRetry = true
		case !record.RetryAt.IsZero():
			return verdict{reason: ReasonRetryWait, next: record.RetryAt}
		default:
			return verdict{reason: ReasonCooldown, next: record.Next}
		}
	}

	for _, b := range bucketsFor(repo, target, limits) {
		count, free := l.used(b.key, now, b.limit)
		if count < b.limit {
			continue
		}
		if v.due || free.After(v.next) {
			v = verdict{reason: ReasonHourly, next: free}
		}
	}
	if !v.due {
		return v
	}
	for _, b := range bucketsFor(repo, target, limits) {
		v.buckets = append(v.buckets, b.key)
	}
	return v
}

type bucketLimit struct {
	key   string
	limit int
}

func bucketsFor(repo *repoRecord, target Target, limits Limits) []bucketLimit {
	if target.Protocol == SSH {
		return []bucketLimit{{bucketKey(target.Host, bucketSSH), limits.SSHPerHour}}
	}
	authenticated, known := repo.authenticated(HTTPS)
	authed := bucketLimit{bucketKey(target.Host, bucketHTTPS), limits.HTTPSPerHour}
	anonymous := bucketLimit{bucketKey(target.Host, bucketHTTPSAnon), limits.HTTPSUnauthenticatedPerHour}
	switch {
	case known && authenticated:
		return []bucketLimit{authed}
	case known:
		return []bucketLimit{anonymous}
	default:
		return []bucketLimit{authed, anonymous}
	}
}

func cooldown(limits Limits, kind Kind) time.Duration {
	if kind == Push {
		return limits.PushCooldown
	}
	return limits.FetchCooldown
}

func (g *Gate) record(ctx context.Context, target Target, kind Kind, limits Limits, res reservation, counts traceCounts, traceRead bool, opErr error) error {
	unlock, err := g.store.lock(ctx, false)
	if err != nil {
		return err
	}
	defer unlock()
	l, err := g.store.read()
	if err != nil {
		return err
	}
	now := g.now()
	repo := l.repo(target)
	repo.Protocol = target.Protocol
	if traceRead {
		g.countConnections(l, repo, target, res, counts)
	}

	record := repo.kind(kind)
	period := cooldown(limits, kind)
	result := classify(opErr)
	record.Outcome = result
	record.RetryAt = time.Time{}
	if result == outcomeOK {
		record.LastSuccess = now.UTC()
		record.FailedAt = time.Time{}
		record.RetriesUsed = 0
		record.Next = now.Add(g.jitter(period)).UTC()
		return g.store.write(l, now)
	}

	if res.viaRetry {
		record.RetriesUsed++
	} else {
		record.FailedAt = now.UTC()
		record.RetriesUsed = 0
		record.Next = now.Add(g.jitter(period)).UTC()
	}
	if result == outcomeTransport && record.RetriesUsed < limits.Retries {
		window := record.FailedAt.Add(period).Sub(now)
		if window > 0 {
			share := retryMinShare + (retryMaxShare-retryMinShare)*g.rand()
			retryAt := now.Add(time.Duration(float64(window) * share))
			if retryAt.After(record.Next) {
				retryAt = record.Next
			}
			record.RetryAt = retryAt.UTC()
		}
	}
	return g.store.write(l, now)
}

// countConnections replaces the reservation with what the trace saw. A
// credential helper means the server asked for a login after an anonymous
// first request, which counts once against the unauthenticated bucket until
// the repository is known to need a login.
func (g *Gate) countConnections(l *ledger, repo *repoRecord, target Target, res reservation, counts traceCounts) {
	for _, key := range res.buckets {
		l.add(key, res.minute, -1)
	}
	if counts.ssh > 0 {
		l.add(bucketKey(target.Host, bucketSSH), res.minute, counts.ssh)
	}
	if counts.https == 0 {
		return
	}
	wasAuthenticated, known := repo.authenticated(HTTPS)
	if counts.credentialHelper {
		l.add(bucketKey(target.Host, bucketHTTPS), res.minute, counts.https)
		if !known || !wasAuthenticated {
			l.add(bucketKey(target.Host, bucketHTTPSAnon), res.minute, 1)
		}
	} else {
		l.add(bucketKey(target.Host, bucketHTTPSAnon), res.minute, counts.https)
	}
	if target.Protocol == HTTPS {
		authenticated := counts.credentialHelper
		repo.Authenticated = &authenticated
	}
}

func (g *Gate) jitter(period time.Duration) time.Duration {
	factor := 1 - jitterSpread + 2*jitterSpread*g.rand()
	return time.Duration(float64(period) * factor)
}

// acquireSlot takes one of the host's slot files. flock ties a slot to an open
// file, so the OS frees the slot of a crashed process; a failed TryLock closes
// its file again.
func (g *Gate) acquireSlot(ctx context.Context, target Target, kind Kind, limits Limits) (func(), error) {
	dir := filepath.Join(g.stateDir, "git-slots")
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return nil, fmt.Errorf("creating git slot directory: %w", err)
	}
	slots := make([]*flock.Flock, 0, limits.Concurrency)
	for n := range limits.Concurrency {
		slots = append(slots, flock.New(filepath.Join(dir, slotName(target.Host)+"."+strconv.Itoa(n)+".lock")))
	}
	timer := time.NewTimer(g.slotWait)
	defer timer.Stop()
	for {
		for _, slot := range slots {
			taken, err := slot.TryLock()
			if err != nil {
				return nil, fmt.Errorf("taking git slot %s: %w", slot.Path(), err)
			}
			if taken {
				return func() { _ = slot.Unlock() }, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, &DeferredError{Target: target, Kind: kind, Reason: ReasonBusy, Next: g.now()}
		case <-time.After(slotRetryDelay):
		}
	}
}

func slotName(host string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' {
			return r
		}
		return '_'
	}, host)
}

func (g *Gate) newTraceFile() (string, error) {
	dir := filepath.Join(g.stateDir, "traces")
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return "", fmt.Errorf("creating git trace directory: %w", err)
	}
	file, err := os.CreateTemp(dir, "*.json")
	if err != nil {
		return "", fmt.Errorf("creating git trace file: %w", err)
	}
	err = file.Close()
	if err != nil {
		return "", fmt.Errorf("creating git trace file: %w", err)
	}
	return file.Name(), nil
}

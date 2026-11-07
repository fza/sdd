package gitgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/gofrs/flock"
)

const (
	ledgerVersion   = 1
	ledgerFile      = "remotes.json"
	lockRetryDelay  = 10 * time.Millisecond
	windowMinutes   = 60
	bucketSSH       = "ssh"
	bucketHTTPS     = "https"
	bucketHTTPSAnon = "https_unauthenticated"
)

// ledger is remotes.json. A file of another version reads as empty and is
// rewritten whole: an older binary drops what a newer one recorded rather than
// misreading it.
type ledger struct {
	Version int                    `json:"version"`
	Buckets map[string]*bucket     `json:"buckets"`
	Repos   map[string]*repoRecord `json:"repos"`
}

type bucket struct {
	Minutes map[string]int `json:"minutes"`
}

type repoRecord struct {
	Protocol      Protocol    `json:"protocol,omitempty"`
	Authenticated *bool       `json:"authenticated,omitempty"`
	Fetch         *kindRecord `json:"fetch,omitempty"`
	Push          *kindRecord `json:"push,omitempty"`
}

type kindRecord struct {
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	LastSuccess time.Time `json:"last_success,omitzero"`
	Outcome     outcome   `json:"outcome,omitempty"`
	FailedAt    time.Time `json:"failed_at,omitzero"`
	RetriesUsed int       `json:"retries_used"`
	RetryAt     time.Time `json:"retry_at,omitzero"`
	Next        time.Time `json:"next,omitzero"`
}

func emptyLedger() *ledger {
	return &ledger{Version: ledgerVersion, Buckets: map[string]*bucket{}, Repos: map[string]*repoRecord{}}
}

func (l *ledger) repo(target Target) *repoRecord {
	record, ok := l.Repos[target.Repo]
	if !ok {
		record = &repoRecord{}
		l.Repos[target.Repo] = record
	}
	return record
}

func (r *repoRecord) kind(kind Kind) *kindRecord {
	slot := &r.Fetch
	if kind == Push {
		slot = &r.Push
	}
	if *slot == nil {
		*slot = &kindRecord{}
	}
	return *slot
}

// authenticated reports whether an HTTPS repository needs a login, known only
// once a trace over the same protocol said so.
func (r *repoRecord) authenticated(protocol Protocol) (value, known bool) {
	if r.Authenticated == nil || r.Protocol != protocol {
		return false, false
	}
	return *r.Authenticated, true
}

func bucketKey(host, name string) string {
	return host + "/" + name
}

func unixMinute(at time.Time) int64 {
	return at.Unix() / 60
}

func (l *ledger) add(key string, minute int64, delta int) {
	b, ok := l.Buckets[key]
	if !ok {
		b = &bucket{Minutes: map[string]int{}}
		l.Buckets[key] = b
	}
	if b.Minutes == nil {
		b.Minutes = map[string]int{}
	}
	field := strconv.FormatInt(minute, 10)
	b.Minutes[field] += delta
	if b.Minutes[field] <= 0 {
		delete(b.Minutes, field)
	}
}

// used counts the bucket's last 60 minutes and, when that reaches limit,
// returns when enough of them age out to leave room for one more.
func (l *ledger) used(key string, now time.Time, limit int) (count int, free time.Time) {
	b, ok := l.Buckets[key]
	if !ok {
		return 0, now
	}
	current := unixMinute(now)
	perMinute := map[int64]int{}
	var minutes []int64
	for field, n := range b.Minutes {
		minute, err := strconv.ParseInt(field, 10, 64)
		if err != nil || minute <= current-windowMinutes {
			continue
		}
		if _, seen := perMinute[minute]; !seen {
			minutes = append(minutes, minute)
		}
		perMinute[minute] += n
		count += n
	}
	if count < limit {
		return count, now
	}
	slices.Sort(minutes)
	remaining := count
	for _, minute := range minutes {
		remaining -= perMinute[minute]
		if remaining < limit {
			return count, time.Unix((minute+windowMinutes)*60, 0).UTC()
		}
	}
	return count, now
}

func (l *ledger) prune(now time.Time) {
	oldest := unixMinute(now) - windowMinutes
	for key, b := range l.Buckets {
		for field := range b.Minutes {
			minute, err := strconv.ParseInt(field, 10, 64)
			if err != nil || minute <= oldest {
				delete(b.Minutes, field)
			}
		}
		if len(b.Minutes) == 0 {
			delete(l.Buckets, key)
		}
	}
}

type ledgerStore struct {
	dir string
}

func (s ledgerStore) path() string {
	return filepath.Join(s.dir, ledgerFile)
}

func (s ledgerStore) lock(ctx context.Context, shared bool) (func(), error) {
	err := os.MkdirAll(s.dir, 0o700)
	if err != nil {
		return nil, fmt.Errorf("creating git gate state directory: %w", err)
	}
	lock := flock.New(s.path() + ".lock")
	take := lock.TryLockContext
	if shared {
		take = lock.TryRLockContext
	}
	_, err = take(ctx, lockRetryDelay)
	if err != nil {
		return nil, fmt.Errorf("locking %s: %w", s.path(), err)
	}
	return func() { _ = lock.Unlock() }, nil
}

func (s ledgerStore) read() (*ledger, error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return emptyLedger(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path(), err)
	}
	var version struct {
		Version int `json:"version"`
	}
	err = json.Unmarshal(data, &version)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path(), err)
	}
	if version.Version != ledgerVersion {
		return emptyLedger(), nil
	}
	l := emptyLedger()
	err = json.Unmarshal(data, l)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", s.path(), err)
	}
	if l.Buckets == nil {
		l.Buckets = map[string]*bucket{}
	}
	if l.Repos == nil {
		l.Repos = map[string]*repoRecord{}
	}
	return l, nil
}

func (s ledgerStore) write(l *ledger, now time.Time) error {
	l.Version = ledgerVersion
	l.prune(now)
	data, err := json.Marshal(l)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", s.path(), err)
	}
	temp, err := os.CreateTemp(s.dir, ledgerFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", s.path(), err)
	}
	_, err = temp.Write(data)
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp.Name(), s.path())
	}
	if err != nil {
		_ = os.Remove(temp.Name())
		return fmt.Errorf("writing %s: %w", s.path(), err)
	}
	return nil
}

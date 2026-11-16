// Package cacheprune owns the on-disk record of cache pruning under the state
// root: when a background run was last launched, when a run last started and
// finished, what it removed, and the failure no command has shown yet.
// Commands read it to decide whether to launch a run and to report a failed
// one once; sdd info reads it.
package cacheprune

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"

	"github.com/networkteam/sdd/internal/model"
)

const (
	statusFile = "cache-prune.json"
	// RunLockFile admits one pruning run per machine.
	RunLockFile = "cache-prune.lock"
	updateLock  = "cache-prune.json.lock"
	// LogFile receives the background run's output, for the failures it
	// cannot record in the status file.
	LogFile = "cache-prune.log"
)

// updateWait bounds how long a command waits for another process updating
// the record; the update itself is one small file write.
const updateWait = time.Second

type wireCounts struct {
	Versions   int   `json:"versions,omitempty"`
	Stores     int   `json:"stores,omitempty"`
	Snapshots  int   `json:"snapshots,omitempty"`
	Clones     int   `json:"clones,omitempty"`
	Records    int   `json:"records,omitempty"`
	Leftovers  int   `json:"leftovers,omitempty"`
	Sessions   int   `json:"sessions,omitempty"`
	Projects   int   `json:"projects,omitempty"`
	Unreadable int   `json:"unreadable_sessions,omitempty"`
	Bytes      int64 `json:"bytes,omitempty"`
}

func toWire(c model.CachePruneCounts) wireCounts {
	return wireCounts{
		Versions: c.Versions, Stores: c.Stores, Snapshots: c.Snapshots, Clones: c.Clones, Records: c.Records,
		Leftovers: c.Leftovers, Sessions: c.Sessions, Projects: c.Projects, Unreadable: c.Unreadable, Bytes: c.Bytes,
	}
}

func (w wireCounts) counts() model.CachePruneCounts {
	return model.CachePruneCounts{
		Versions: w.Versions, Stores: w.Stores, Snapshots: w.Snapshots, Clones: w.Clones, Records: w.Records,
		Leftovers: w.Leftovers, Sessions: w.Sessions, Projects: w.Projects, Unreadable: w.Unreadable, Bytes: w.Bytes,
	}
}

type wireStatus struct {
	LaunchedAt time.Time  `json:"launched_at,omitzero"`
	StartedAt  time.Time  `json:"started_at,omitzero"`
	FinishedAt time.Time  `json:"finished_at,omitzero"`
	Removed    wireCounts `json:"removed,omitzero"`
	Error      string     `json:"error,omitempty"`
	Reported   bool       `json:"reported,omitempty"`
}

// Load reads the record under stateRoot; a missing record is the zero status.
// An unreadable record reads as a failure to report, never as an error: it
// only caches run results, and refusing it would stop every later run and
// `sdd info` until someone deleted it by hand. The next Update replaces it.
func Load(stateRoot string) (model.CachePruneStatus, error) {
	data, err := os.ReadFile(filepath.Join(stateRoot, statusFile))
	if errors.Is(err, fs.ErrNotExist) {
		return model.CachePruneStatus{}, nil
	}
	if err != nil {
		return model.CachePruneStatus{}, fmt.Errorf("reading cache pruning status: %w", err)
	}
	var w wireStatus
	if err := json.Unmarshal(data, &w); err != nil {
		unreadable := model.CachePruneStatus{Error: fmt.Sprintf("unreadable status record %s: %v", filepath.Join(stateRoot, statusFile), err)}
		if info, statErr := os.Stat(filepath.Join(stateRoot, statusFile)); statErr == nil {
			unreadable.FinishedAt = info.ModTime()
		}
		return unreadable, nil
	}
	return model.CachePruneStatus{
		LaunchedAt: w.LaunchedAt, StartedAt: w.StartedAt, FinishedAt: w.FinishedAt,
		Removed: w.Removed.counts(), Error: w.Error, Reported: w.Reported,
	}, nil
}

// Update applies change to the record under stateRoot as one locked
// read-modify-write, so a launch, a run recording its start and its result,
// and a command marking a failure reported do not overwrite each other.
func Update(ctx context.Context, stateRoot string, change func(*model.CachePruneStatus)) (err error) {
	if err := os.MkdirAll(stateRoot, 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}
	lock := flock.New(filepath.Join(stateRoot, updateLock))
	waitCtx, cancel := context.WithTimeout(ctx, updateWait)
	defer cancel()
	if _, err := lock.TryLockContext(waitCtx, 10*time.Millisecond); err != nil {
		return fmt.Errorf("locking cache pruning status: %w", err)
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	status, err := Load(stateRoot)
	if err != nil {
		return err
	}
	change(&status)
	return save(stateRoot, status)
}

func save(stateRoot string, s model.CachePruneStatus) error {
	data, err := json.MarshalIndent(wireStatus{
		LaunchedAt: s.LaunchedAt, StartedAt: s.StartedAt, FinishedAt: s.FinishedAt,
		Removed: toWire(s.Removed), Error: s.Error, Reported: s.Reported,
	}, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(stateRoot, statusFile)
	temp := final + ".tmp"
	if err := writeSynced(temp, append(data, '\n')); err != nil {
		return fmt.Errorf("writing cache pruning status: %w", err)
	}
	if err := os.Rename(temp, final); err != nil {
		return fmt.Errorf("writing cache pruning status: %w", err)
	}
	return nil
}

// LaunchIfDue starts a background run through launch when the last one is a
// day or more ago, recording the launch first so the commands that follow do
// not start another. A launch that fails is recorded as the run's failure, so
// it is reported once like any other.
func LaunchIfDue(ctx context.Context, stateRoot string, now time.Time, launch func() error) error {
	status, err := Load(stateRoot)
	if err != nil {
		return err
	}
	if !status.Due(now) {
		return nil
	}
	due := false
	err = Update(ctx, stateRoot, func(s *model.CachePruneStatus) {
		// Judged again under the lock, so of two commands that both read
		// the run as due only one launches it.
		if due = s.Due(now); due {
			s.LaunchedAt = now
		}
	})
	if err != nil || !due {
		return err
	}
	launchErr := launch()
	if launchErr == nil {
		return nil
	}
	return RecordFailure(ctx, stateRoot, now, fmt.Errorf("starting the run: %w", launchErr))
}

// RecordFailure records failure as the latest run's, ended at now, for the
// next command to report.
func RecordFailure(ctx context.Context, stateRoot string, now time.Time, failure error) error {
	return Update(ctx, stateRoot, func(s *model.CachePruneStatus) {
		s.FinishedAt, s.Error, s.Reported = now, failure.Error(), false
	})
}

// TakeFailureNotice returns the notice for a failed run no command has shown
// yet and marks it shown; empty when there is none.
func TakeFailureNotice(ctx context.Context, stateRoot string) (string, error) {
	status, err := Load(stateRoot)
	if err != nil {
		return "", err
	}
	if status.FailureNotice() == "" {
		return "", nil
	}
	var notice string
	err = Update(ctx, stateRoot, func(s *model.CachePruneStatus) {
		notice = s.FailureNotice()
		s.Reported = true
	})
	return notice, err
}

// writeSynced writes data to path and syncs it, so a crash before the rename
// cannot leave the record empty.
func writeSynced(path string, data []byte) (err error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

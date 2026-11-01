// Package cacheprune owns the on-disk record of background cache pruning
// (20261101-131514-d-cpt-9ci) under the state root: when a run was last
// launched and started, what it removed, and the failure no command has shown
// yet. Commands read it to decide whether to launch a run and to report a
// failed one once.
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
)

// updateWait bounds how long a command waits for another process updating
// the record; the update itself is one small file write.
const updateWait = time.Second

type wireCounts struct {
	Versions  int   `json:"versions,omitempty"`
	Stores    int   `json:"stores,omitempty"`
	Snapshots int   `json:"snapshots,omitempty"`
	Clones    int   `json:"clones,omitempty"`
	Bytes     int64 `json:"bytes,omitempty"`
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
		return model.CachePruneStatus{}, fmt.Errorf("parsing cache pruning status %s: %w", filepath.Join(stateRoot, statusFile), err)
	}
	return model.CachePruneStatus{
		LaunchedAt: w.LaunchedAt, StartedAt: w.StartedAt, FinishedAt: w.FinishedAt,
		Removed: model.CachePruneCounts{
			Versions: w.Removed.Versions, Stores: w.Removed.Stores, Snapshots: w.Removed.Snapshots,
			Clones: w.Removed.Clones, Bytes: w.Removed.Bytes,
		},
		Error: w.Error, Reported: w.Reported,
	}, nil
}

// Update applies change to the record under stateRoot as one locked
// read-modify-write, so a command marking a failure reported and a run
// recording its result do not overwrite each other.
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
		Removed: wireCounts{
			Versions: s.Removed.Versions, Stores: s.Removed.Stores, Snapshots: s.Removed.Snapshots,
			Clones: s.Removed.Clones, Bytes: s.Removed.Bytes,
		},
		Error: s.Error, Reported: s.Reported,
	}, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(stateRoot, statusFile)
	temp := final + ".tmp"
	if err := os.WriteFile(temp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing cache pruning status: %w", err)
	}
	if err := os.Rename(temp, final); err != nil {
		return fmt.Errorf("writing cache pruning status: %w", err)
	}
	return nil
}

// LaunchIfDue starts a background run through launch when the last one is a
// day or more ago, recording the launch first so the commands that follow do
// not start another.
func LaunchIfDue(ctx context.Context, stateRoot string, now time.Time, launch func() error) error {
	status, err := Load(stateRoot)
	if err != nil {
		return err
	}
	if !status.Due(now) {
		return nil
	}
	if err := Update(ctx, stateRoot, func(s *model.CachePruneStatus) { s.LaunchedAt = now }); err != nil {
		return err
	}
	if err := launch(); err != nil {
		return fmt.Errorf("starting background cache pruning: %w", err)
	}
	return nil
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

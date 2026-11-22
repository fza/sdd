package local

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

	sdd "github.com/networkteam/sdd/pkg/application"
)

const (
	calibrationLockFile  = "auto-calibration.lock"
	calibrationStateFile = "auto-calibration.json"
)

// FilesystemCalibrationRuns admits one background calibration per directory
// through an exclusive lock on a file in it, so a run ends with the process
// that held it, and keeps the holder's record beside the lock.
type FilesystemCalibrationRuns struct {
	dir string
}

// NewFilesystemCalibrationRuns returns the runs kept in dir. Nothing is
// created until the first Acquire.
func NewFilesystemCalibrationRuns(dir string) (*FilesystemCalibrationRuns, error) {
	if dir == "" {
		return nil, fmt.Errorf("sdd: calibration run directory is required")
	}
	return &FilesystemCalibrationRuns{dir: dir}, nil
}

func (r *FilesystemCalibrationRuns) Acquire(_ context.Context) (sdd.CalibrationLease, bool, error) {
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return nil, false, fmt.Errorf("acquiring the calibration run: %w", err)
	}
	lock := flock.New(filepath.Join(r.dir, calibrationLockFile))
	held, err := lock.TryLock()
	if err != nil {
		return nil, false, fmt.Errorf("acquiring the calibration run: %w", err)
	}
	if !held {
		return nil, false, nil
	}
	return &fileCalibrationLease{lock: lock, path: filepath.Join(r.dir, calibrationStateFile)}, true, nil
}

func (r *FilesystemCalibrationRuns) Running(_ context.Context) (sdd.CalibrationRun, bool, error) {
	path := filepath.Join(r.dir, calibrationLockFile)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return sdd.CalibrationRun{}, false, nil
	}
	lock := flock.New(path)
	free, err := lock.TryRLock()
	if err != nil {
		return sdd.CalibrationRun{}, false, fmt.Errorf("reading the calibration run: %w", err)
	}
	if free {
		return sdd.CalibrationRun{}, false, lock.Unlock()
	}
	data, err := os.ReadFile(filepath.Join(r.dir, calibrationStateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return sdd.CalibrationRun{}, true, nil
	}
	if err != nil {
		return sdd.CalibrationRun{}, false, fmt.Errorf("reading the calibration run: %w", err)
	}
	var wire calibrationRunWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return sdd.CalibrationRun{}, false, fmt.Errorf("reading the calibration run %s: %w", filepath.Join(r.dir, calibrationStateFile), err)
	}
	return sdd.CalibrationRun(wire), true, nil
}

type calibrationRunWire struct {
	Judge     string    `json:"judge"`
	Started   time.Time `json:"started"`
	Stage     float64   `json:"stage,omitempty"`
	Scored    int       `json:"scored,omitempty"`
	Waiting   bool      `json:"waiting,omitempty"`
	Replaying bool      `json:"replaying,omitempty"`
}

type fileCalibrationLease struct {
	lock *flock.Flock
	path string
}

func (l *fileCalibrationLease) Record(_ context.Context, run sdd.CalibrationRun) (err error) {
	data, err := json.MarshalIndent(calibrationRunWire(run), "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(l.path), ".auto-calibration-*.tmp")
	if err != nil {
		return fmt.Errorf("recording the calibration run: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmp.Name()))
		}
	}()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		return errors.Join(fmt.Errorf("recording the calibration run: %w", err), tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("recording the calibration run: %w", err)
	}
	if err := os.Rename(tmp.Name(), l.path); err != nil {
		return fmt.Errorf("recording the calibration run: %w", err)
	}
	return nil
}

func (l *fileCalibrationLease) Release() error {
	err := os.Remove(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
	}
	return errors.Join(err, l.lock.Unlock())
}

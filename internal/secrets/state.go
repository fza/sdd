package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CacheDirName is the folder under sdd's cache root that holds the fetched
// rule files and their fetch state.
const CacheDirName = "secret-rules"

const stateFileName = "state.json"

// fetchState is the fetch record beside the fetched copies: which copy was
// accepted last and how the latest attempts went.
type fetchState struct {
	Tag        string    `json:"tag,omitempty"`
	File       string    `json:"file,omitempty"`
	SHA256     string    `json:"sha256,omitempty"`
	AcceptedAt time.Time `json:"accepted_at,omitzero"`
	CheckedAt  time.Time `json:"checked_at,omitzero"`
	Failure    string    `json:"failure,omitempty"`
	FailedAt   time.Time `json:"failed_at,omitzero"`
}

func readState(dir string) (fetchState, error) {
	var state fetchState
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("reading the rule fetch state: %w", err)
	}
	err = json.Unmarshal(data, &state)
	if err != nil {
		return fetchState{}, fmt.Errorf("decoding the rule fetch state: %w", err)
	}
	return state, nil
}

func writeState(dir string, state fetchState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, stateFileName, append(data, '\n'))
}

// stateStamp tells a changed fetch state from the one a source last read.
type stateStamp struct {
	modTime time.Time
	size    int64
}

func statState(dir string) (stateStamp, error) {
	info, err := os.Stat(filepath.Join(dir, stateFileName))
	if errors.Is(err, fs.ErrNotExist) {
		return stateStamp{}, nil
	}
	if err != nil {
		return stateStamp{}, err
	}
	return stateStamp{modTime: info.ModTime(), size: info.Size()}, nil
}

const tempPattern = ".write-*.tmp"

// writeFileAtomic replaces dir/name in one rename, so a reader sees the old
// file or the new one, never part of either.
func writeFileAtomic(dir, name string, data []byte) (err error) {
	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, tempPattern)
	if err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(tmp.Name()))
		}
	}()
	_, err = tmp.Write(data)
	if err != nil {
		return errors.Join(fmt.Errorf("writing %s: %w", name, err), tmp.Close())
	}
	err = tmp.Close()
	if err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	err = os.Rename(tmp.Name(), filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	return nil
}

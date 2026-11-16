package gitgate

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type traceCounts struct {
	ssh              int
	https            int
	credentialHelper bool
}

// readTrace counts the connections an op opened from its GIT_TRACE2_EVENT
// file. A missing file means no git process ran.
func readTrace(path string) (traceCounts, error) {
	var counts traceCounts
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return counts, nil
	}
	if err != nil {
		return counts, fmt.Errorf("reading git trace: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		err := json.Unmarshal(scanner.Bytes(), &event)
		if err != nil {
			return counts, fmt.Errorf("reading git trace %s: %w", path, err)
		}
		if event.Event != "child_start" || len(event.Argv) == 0 {
			continue
		}
		counts.add(event.Argv[0])
	}
	err = scanner.Err()
	if err != nil {
		return counts, fmt.Errorf("reading git trace %s: %w", path, err)
	}
	return counts, nil
}

// add takes the first word of argv[0], since git logs a shell command such as
// GIT_SSH_COMMAND whole. The ["git","remote-https"] wrapper is not counted:
// the git-remote-https child it starts is.
func (c *traceCounts) add(argv0 string) {
	fields := strings.Fields(argv0)
	if len(fields) == 0 {
		return
	}
	name := strings.TrimSuffix(filepath.Base(fields[0]), ".exe")
	switch {
	case name == "git-remote-https" || name == "git-remote-http":
		c.https++
	case name == "ssh":
		c.ssh++
	case strings.HasPrefix(name, "git-credential-"):
		c.credentialHelper = true
	}
}

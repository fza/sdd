package roundproc

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// startStamp reads the start time of the process with pid, in clock ticks
// since boot, and whether it runs.
func startStamp(pid int) (string, bool, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	// The command name in parentheses may hold spaces; the fields after it
	// start with the state, the 3rd field, and hold the start time as the 22nd.
	rest := string(stat)
	if i := strings.LastIndexByte(rest, ')'); i >= 0 {
		rest = rest[i+1:]
	}
	fields := strings.Fields(rest)
	if len(fields) < 20 {
		return "", false, fmt.Errorf("unexpected /proc/%d/stat: %q", pid, stat)
	}
	if state := fields[0]; state == "Z" || state == "X" {
		return "", false, nil
	}
	return fields[19], true, nil
}

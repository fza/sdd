package roundproc

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// inspect reads the process with pid; its start stamp is its start time in
// clock ticks since boot.
func inspect(pid int) (state, error) {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if errors.Is(err, fs.ErrNotExist) {
		return state{}, nil
	}
	if err != nil {
		return state{}, err
	}
	// The command name in parentheses may hold spaces; the fields after it
	// start with the state, the 3rd field, and hold the process group as the
	// 5th and the start time as the 22nd.
	rest := string(stat)
	if i := strings.LastIndexByte(rest, ')'); i >= 0 {
		rest = rest[i+1:]
	}
	fields := strings.Fields(rest)
	if len(fields) < 20 {
		return state{}, fmt.Errorf("unexpected /proc/%d/stat: %q", pid, stat)
	}
	if status := fields[0]; status == "Z" || status == "X" {
		return state{}, nil
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return state{}, fmt.Errorf("unexpected process group in /proc/%d/stat: %q", pid, stat)
	}
	return state{running: true, started: fields[19], group: group}, nil
}

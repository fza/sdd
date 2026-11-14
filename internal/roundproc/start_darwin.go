package roundproc

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// zombie is SZOMB of sys/proc.h: a process that ended and was not reaped.
const zombie = 5

// startStamp reads the start time of the process with pid, and whether it
// runs.
func startStamp(pid int) (string, bool, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return "", false, err
	}
	if len(procs) == 0 || procs[0].Proc.P_stat == zombie {
		return "", false, nil
	}
	start := procs[0].Proc.P_starttime
	return fmt.Sprintf("%d.%06d", start.Sec, start.Usec), true, nil
}

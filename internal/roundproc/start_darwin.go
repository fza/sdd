package roundproc

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// zombie is SZOMB of sys/proc.h: a process that ended and was not reaped.
const zombie = 5

// inspect reads the process with pid.
func inspect(pid int) (state, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pid", pid)
	if err != nil {
		return state{}, err
	}
	if len(procs) == 0 || procs[0].Proc.P_stat == zombie {
		return state{}, nil
	}
	start := procs[0].Proc.P_starttime
	return state{running: true, started: fmt.Sprintf("%d.%06d", start.Sec, start.Usec), group: int(procs[0].Eproc.Pgid)}, nil
}

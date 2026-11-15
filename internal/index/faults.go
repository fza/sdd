package index

import (
	"fmt"
	"runtime/debug"
)

// guardFaults runs fn with memory faults turned into an error. Segment files
// are only ever created by rename and removed by unlink, and a mapping lives
// until the last reader releases it, so sdd itself never faults on one; a file
// another program truncates or a failing disk does, and that must fail the
// search rather than the process. Any other panic propagates unchanged. fn
// must touch mapped memory on the calling goroutine only.
func guardFaults(fn func() error) (err error) {
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		fault, ok := recovered.(interface{ Addr() uintptr })
		if !ok {
			panic(recovered)
		}
		err = fmt.Errorf("%w: memory fault at %#x", ErrSegmentCorrupt, fault.Addr())
	}()
	return fn()
}

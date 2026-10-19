// Package filestamp reads what a file's metadata proves about its content: a
// stamp equal to an earlier one means the file was not written since, unless
// the earlier stamp was taken within RacyWindow of a change.
package filestamp

import (
	"io/fs"
	"time"
)

// RacyWindow is how recent a file's change may be before its stamp stops
// proving its content: a write in the same clock tick as the stamp leaves the
// stamp unchanged, so a file changed this close to a stamp is read again next
// time. Two seconds covers the coarsest common filesystem timestamps.
const RacyWindow = 2 * time.Second

// Stamp is a file's identity and change times. Ctime cannot be set by a
// program, so a write that restores size and mtime still changes it. Trusted
// is false where the platform gives no inode and ctime; such a stamp proves
// nothing.
type Stamp struct {
	Size    int64
	Mode    fs.FileMode
	Mtime   int64
	Ctime   int64
	Inode   uint64
	Device  uint64
	Trusted bool
}

// Proves reports whether the stamp, taken at now, can stand in for the file's
// content later: trusted, and not changed within RacyWindow of now.
func (s Stamp) Proves(now time.Time) bool {
	return s.Trusted && now.Sub(time.Unix(0, max(s.Mtime, s.Ctime))) >= RacyWindow
}

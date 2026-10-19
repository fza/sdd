//go:build !darwin && !linux

package local

import "io/fs"

// stampOf leaves the stamp untrusted where no inode and ctime are read, so the
// revision is always computed from content there.
func stampOf(info fs.FileInfo) fileStamp {
	return fileStamp{size: info.Size(), mode: info.Mode(), mtime: info.ModTime().UnixNano()}
}

package local

import (
	"io/fs"
	"syscall"
)

func stampOf(info fs.FileInfo) fileStamp {
	stamp := fileStamp{size: info.Size(), mode: info.Mode(), mtime: info.ModTime().UnixNano()}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return stamp
	}
	stamp.ctime = sys.Ctimespec.Nano()
	stamp.inode, stamp.device = sys.Ino, uint64(sys.Dev)
	stamp.trusted = true
	return stamp
}

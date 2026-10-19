package filestamp

import (
	"io/fs"
	"syscall"
)

// Of stamps the file info describes.
func Of(info fs.FileInfo) Stamp {
	stamp := Stamp{Size: info.Size(), Mode: info.Mode(), Mtime: info.ModTime().UnixNano()}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return stamp
	}
	stamp.Ctime = sys.Ctim.Nano()
	stamp.Inode, stamp.Device = sys.Ino, uint64(sys.Dev)
	stamp.Trusted = true
	return stamp
}

//go:build !darwin && !linux

package filestamp

import "io/fs"

// Of stamps the file info describes. Without inode and ctime the stamp is
// untrusted, so callers read the content.
func Of(info fs.FileInfo) Stamp {
	return Stamp{Size: info.Size(), Mode: info.Mode(), Mtime: info.ModTime().UnixNano()}
}

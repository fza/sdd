//go:build unix

package index

import (
	"os"

	"golang.org/x/sys/unix"
)

// mapSegment maps the whole segment file read-only and shared, so every
// process searching the store reads the same page-cache pages and a search
// copies no vector into its heap. A deleted file stays mapped until release.
func mapSegment(file *os.File, size int) ([]byte, func() error, error) {
	if size == 0 {
		return nil, func() error { return nil }, nil
	}
	data, err := unix.Mmap(int(file.Fd()), 0, size, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		return nil, nil, err
	}
	return data, func() error { return unix.Munmap(data) }, nil
}

// adviseWillNeed asks the kernel to read region ahead in one sweep, so a scan
// over a cold mapping costs one sequential read rather than a fault per page.
// region must start inside a mapping returned by mapSegment.
func adviseWillNeed(mapping []byte, start, end int) {
	pageSize := os.Getpagesize()
	start -= start % pageSize
	if start >= end {
		return
	}
	// A refused hint changes how fast pages arrive, never what they hold.
	_ = unix.Madvise(mapping[start:end], unix.MADV_WILLNEED)
}

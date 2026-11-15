//go:build !unix

package index

import (
	"io"
	"os"
)

func mapSegment(file *os.File, size int) ([]byte, func() error, error) {
	data := make([]byte, size)
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, nil, err
	}
	return data, func() error { return nil }, nil
}

func adviseWillNeed([]byte, int, int) {}

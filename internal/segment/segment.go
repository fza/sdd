// Package segment is the binary codec of sdd's append logs: immutable segment
// files named <time>-<random>.seg, each holding the records of one write
// (20261109-004506-d-cpt-udz, D15 and D17). The project event store writes
// its events through it; the relevance ledger shares it.
//
//	magic    "SDDREC"
//	version  one byte
//	kind     uvarint length, bytes: which record schema the segment holds
//	records  per record: uvarint length, bytes
//	checksum CRC-32C of every byte before it, little-endian
//
// A segment is written whole and never rewritten, so the checksum covers the
// file and a torn or edited one reads as corrupt instead of as fewer records.
package segment

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"time"
)

// Ext is the file extension of a segment.
const Ext = ".seg"

const (
	version      = 1
	checksumSize = 4
	nameLayout   = "20060102-150405.000000000"
	randomBytes  = 8
)

var magic = []byte("SDDREC")

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// ErrCorrupt reports bytes that do not decode as a segment of the expected
// kind.
var ErrCorrupt = errors.New("segment is corrupt")

// Encode returns the segment bytes holding records under kind.
func Encode(kind string, records [][]byte) []byte {
	out := append([]byte(nil), magic...)
	out = append(out, version)
	out = AppendString(out, kind)
	for _, record := range records {
		out = AppendBytes(out, record)
	}
	return binary.LittleEndian.AppendUint32(out, crc32.Checksum(out, castagnoli))
}

// Decode returns the records of a segment written under kind. The records
// alias data.
func Decode(data []byte, kind string) ([][]byte, error) {
	if len(data) < len(magic)+1+checksumSize || string(data[:len(magic)]) != string(magic) {
		return nil, fmt.Errorf("%w: no segment header", ErrCorrupt)
	}
	body, sum := data[:len(data)-checksumSize], data[len(data)-checksumSize:]
	if crc32.Checksum(body, castagnoli) != binary.LittleEndian.Uint32(sum) {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrCorrupt)
	}
	if body[len(magic)] != version {
		return nil, fmt.Errorf("%w: format version %d, this sdd reads %d", ErrCorrupt, body[len(magic)], version)
	}
	reader := NewReader(body[len(magic)+1:])
	gotKind, err := reader.String()
	if err != nil {
		return nil, err
	}
	if gotKind != kind {
		return nil, fmt.Errorf("%w: holds %q records, want %q", ErrCorrupt, gotKind, kind)
	}
	var records [][]byte
	for !reader.Empty() {
		record, err := reader.Bytes()
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// Name returns a fresh segment file name for a write at the given time. The
// random part keeps names from two writers apart, so a directory of segments
// merges in Git without conflicts.
func Name(at time.Time) (string, error) {
	random := make([]byte, randomBytes)
	_, err := rand.Read(random)
	if err != nil {
		return "", fmt.Errorf("drawing a segment name: %w", err)
	}
	return at.UTC().Format(nameLayout) + "-" + hex.EncodeToString(random) + Ext, nil
}

// IsName reports whether a directory entry name is a segment, not a
// temporary file or another file beside the segments.
func IsName(name string) bool {
	return strings.HasSuffix(name, Ext) && !strings.HasPrefix(name, ".")
}

// AppendUvarint appends v as a uvarint.
func AppendUvarint(dst []byte, v uint64) []byte { return binary.AppendUvarint(dst, v) }

// AppendVarint appends v as a signed varint.
func AppendVarint(dst []byte, v int64) []byte { return binary.AppendVarint(dst, v) }

// AppendBytes appends b with its uvarint length prefix.
func AppendBytes(dst, b []byte) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(b)))
	return append(dst, b...)
}

// AppendString appends s with its uvarint length prefix.
func AppendString(dst []byte, s string) []byte {
	dst = binary.AppendUvarint(dst, uint64(len(s)))
	return append(dst, s...)
}

// Reader reads the fields of one record in the order they were appended.
type Reader struct{ data []byte }

// NewReader reads fields from data.
func NewReader(data []byte) *Reader { return &Reader{data: data} }

// Empty reports whether every byte has been read.
func (r *Reader) Empty() bool { return len(r.data) == 0 }

// Uvarint reads one uvarint.
func (r *Reader) Uvarint() (uint64, error) {
	value, n := binary.Uvarint(r.data)
	if n <= 0 {
		return 0, fmt.Errorf("%w: truncated uvarint", ErrCorrupt)
	}
	r.data = r.data[n:]
	return value, nil
}

// Varint reads one signed varint.
func (r *Reader) Varint() (int64, error) {
	value, n := binary.Varint(r.data)
	if n <= 0 {
		return 0, fmt.Errorf("%w: truncated varint", ErrCorrupt)
	}
	r.data = r.data[n:]
	return value, nil
}

// Bytes reads one length-prefixed field. The result aliases the reader's
// data.
func (r *Reader) Bytes() ([]byte, error) {
	length, err := r.Uvarint()
	if err != nil {
		return nil, err
	}
	if length > uint64(len(r.data)) {
		return nil, fmt.Errorf("%w: field of %d bytes past the end", ErrCorrupt, length)
	}
	field := r.data[:length]
	r.data = r.data[length:]
	return field, nil
}

// String reads one length-prefixed field as a string.
func (r *Reader) String() (string, error) {
	field, err := r.Bytes()
	return string(field), err
}

// Byte reads one byte.
func (r *Reader) Byte() (byte, error) {
	if len(r.data) == 0 {
		return 0, fmt.Errorf("%w: truncated byte", ErrCorrupt)
	}
	value := r.data[0]
	r.data = r.data[1:]
	return value, nil
}

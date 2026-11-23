// Package segment names the files of sdd's append logs: immutable segments
// named <time>-<random><ext>, each holding the records of one write. A
// segment is written whole and never rewritten, so a torn or edited one reads
// as corrupt instead of as fewer records.
package segment

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	nameLayout  = "20060102-150405.000000000"
	randomBytes = 8
)

// ErrCorrupt reports a segment whose bytes do not read as the records its
// log expects.
var ErrCorrupt = errors.New("segment is corrupt")

// Name returns a fresh segment file name with extension ext for a write at
// the given time. The random part keeps names from two writers apart, so a
// directory of segments merges in Git without conflicts.
func Name(at time.Time, ext string) (string, error) {
	random := make([]byte, randomBytes)
	_, err := rand.Read(random)
	if err != nil {
		return "", fmt.Errorf("drawing a segment name: %w", err)
	}
	return at.UTC().Format(nameLayout) + "-" + hex.EncodeToString(random) + ext, nil
}

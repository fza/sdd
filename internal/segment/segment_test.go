package segment_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"regexp"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/segment"
)

func TestEncodeDecode_RoundTripsRecords(t *testing.T) {
	records := [][]byte{[]byte("first"), {}, bytes.Repeat([]byte{0xff}, 300)}
	data := segment.Encode("events", records)
	decoded, err := segment.Decode(data, "events")
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(records) {
		t.Fatalf("decoded %d records, want %d", len(decoded), len(records))
	}
	for i := range records {
		if !bytes.Equal(decoded[i], records[i]) {
			t.Fatalf("record %d = %x, want %x", i, decoded[i], records[i])
		}
	}
}

func TestEncode_IsDeterministic(t *testing.T) {
	records := [][]byte{[]byte("a"), []byte("b")}
	if !bytes.Equal(segment.Encode("events", records), segment.Encode("events", records)) {
		t.Fatal("the same records encoded to different bytes")
	}
}

func TestDecode_RefusesCorruptSegments(t *testing.T) {
	valid := segment.Encode("events", [][]byte{[]byte("record")})
	flipped := bytes.Clone(valid)
	flipped[len(flipped)-6] ^= 0x01
	otherVersion := bytes.Clone(valid)
	otherVersion[6] = 9
	cases := map[string][]byte{
		"empty":         nil,
		"no magic":      append([]byte("NOTSEG"), valid[6:]...),
		"flipped byte":  flipped,
		"truncated":     valid[:len(valid)-1],
		"other version": resum(otherVersion),
		"long field":    withChecksum([]byte("SDDREC\x01\x06events\x09abc")),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := segment.Decode(data, "events")
			if !errors.Is(err, segment.ErrCorrupt) {
				t.Fatalf("Decode = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestDecode_RefusesAnotherKind(t *testing.T) {
	_, err := segment.Decode(segment.Encode("relevance", nil), "events")
	if !errors.Is(err, segment.ErrCorrupt) {
		t.Fatalf("Decode = %v, want ErrCorrupt for a segment of another kind", err)
	}
}

func TestName_IsTimeThenRandom(t *testing.T) {
	at := time.Date(2026, 11, 9, 0, 45, 6, 123456789, time.FixedZone("x", 3600))
	first, err := segment.Name(at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := segment.Name(at)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^20261108-234506\.123456789-[0-9a-f]{16}\.seg$`).MatchString(first) {
		t.Fatalf("Name = %q, want UTC time then 16 hex characters", first)
	}
	if first == second {
		t.Fatalf("two names for one instant are equal: %q", first)
	}
	if !segment.IsName(first) {
		t.Fatalf("IsName(%q) = false", first)
	}
}

func TestIsName_SkipsOtherFiles(t *testing.T) {
	for _, name := range []string{".gitignore", ".20261108-234506.1-ab.seg.tmp", ".hidden.seg", "notes.txt"} {
		if segment.IsName(name) {
			t.Fatalf("IsName(%q) = true", name)
		}
	}
}

func TestReader_ReadsFieldsInOrder(t *testing.T) {
	record := segment.AppendString(nil, "id")
	record = segment.AppendBytes(record, []byte{1, 2})
	reader := segment.NewReader(record)
	id, err := reader.String()
	if err != nil || id != "id" {
		t.Fatalf("String = %q, %v", id, err)
	}
	payload, err := reader.Bytes()
	if err != nil || !bytes.Equal(payload, []byte{1, 2}) {
		t.Fatalf("Bytes = %x, %v", payload, err)
	}
	if !reader.Empty() {
		t.Fatal("reader not empty after the last field")
	}
	_, err = reader.Uvarint()
	if !errors.Is(err, segment.ErrCorrupt) {
		t.Fatalf("Uvarint past the end = %v, want ErrCorrupt", err)
	}
	_, err = reader.Varint()
	if !errors.Is(err, segment.ErrCorrupt) {
		t.Fatalf("Varint past the end = %v, want ErrCorrupt", err)
	}
}

// resum replaces the trailing checksum, so a case reaches the check after it.
func resum(data []byte) []byte {
	return withChecksum(data[:len(data)-4])
}

func withChecksum(body []byte) []byte {
	body = bytes.Clone(body)
	return binary.LittleEndian.AppendUint32(body, crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)))
}

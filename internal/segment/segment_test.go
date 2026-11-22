package segment_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/segment"
)

func TestName_IsTimeThenRandom(t *testing.T) {
	at := time.Date(2026, 11, 9, 0, 45, 6, 123456789, time.FixedZone("x", 3600))
	first, err := segment.Name(at, ".jsonl")
	if err != nil {
		t.Fatal(err)
	}
	second, err := segment.Name(at, ".jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^20261108-234506\.123456789-[0-9a-f]{16}\.jsonl$`).MatchString(first) {
		t.Fatalf("Name = %q, want UTC time, 16 hex characters, then the extension", first)
	}
	if first == second {
		t.Fatalf("two names for one instant are equal: %q", first)
	}
}

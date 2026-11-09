package sddtest

import (
	"bytes"
	"slices"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// RelevanceStoreFixture drives the conformance of a relevance verdict
// ledger store. Store must start empty.
type RelevanceStoreFixture struct {
	Store sdd.RelevanceStore
}

// RunRelevanceStoreTests checks that a store returns every segment it
// accepted, each byte for byte, repeats included, and shares no memory with
// callers.
func RunRelevanceStoreTests(t *testing.T, factory func(*testing.T) RelevanceStoreFixture) {
	t.Helper()
	t.Run("empty store", func(t *testing.T) {
		segments, err := factory(t).Store.Segments(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(segments) != 0 {
			t.Fatalf("an empty store returned %d segments", len(segments))
		}
	})

	t.Run("keeps every segment", func(t *testing.T) {
		store := factory(t).Store
		written := [][]byte{[]byte("first segment"), {0x00, 0xff, 0x10}, []byte("first segment")}
		for _, data := range written {
			if err := store.Append(t.Context(), data); err != nil {
				t.Fatal(err)
			}
		}
		got, err := store.Segments(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		sortSegments(got)
		want := slices.Clone(written)
		sortSegments(want)
		if len(got) != len(want) {
			t.Fatalf("store returned %d segments, every append should keep its own", len(got))
		}
		for i := range want {
			if !bytes.Equal(got[i], want[i]) {
				t.Fatalf("segment %d = %x: segments should come back byte for byte", i, got[i])
			}
		}
	})

	t.Run("shares no memory with the caller", func(t *testing.T) {
		store := factory(t).Store
		data := []byte("segment")
		if err := store.Append(t.Context(), data); err != nil {
			t.Fatal(err)
		}
		data[0] = 'X'
		got, err := store.Segments(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		got[0][1] = 'Y'
		again, err := store.Segments(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if string(again[0]) != "segment" {
			t.Fatalf("stored segment = %q: edits by a caller should not reach the store", again[0])
		}
	})
}

func sortSegments(segments [][]byte) {
	slices.SortFunc(segments, bytes.Compare)
}

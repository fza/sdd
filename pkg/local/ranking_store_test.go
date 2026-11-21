package local_test

import (
	"encoding/json"
	"slices"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

func rankingTexts(t *testing.T, store sdd.RankingStore, ref sdd.SessionRef, instance string) []string {
	t.Helper()
	records, err := store.Rankings(t.Context(), ref, instance)
	if err != nil {
		t.Fatal(err)
	}
	texts := make([]string, len(records))
	for i, record := range records {
		texts[i] = string(record)
	}
	slices.Sort(texts)
	return texts
}

// The filesystem staged store keeps ranking records per session and instance
// on disk: another store over the same directory reads them, and they go
// with the session's staged area.
func TestFilesystemStagedBlobStoreKeepsRankingRecords(t *testing.T) {
	dir := canonicalTempDir(t)
	store, err := localadapter.NewFilesystemStagedBlobStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref := sdd.SessionRef{Subject: "alice", Session: "s_one"}
	other := sdd.SessionRef{Subject: "alice", Session: "s_two"}
	for _, kept := range []struct {
		ref      sdd.SessionRef
		instance string
		record   string
	}{
		{ref, "i_1", `{"scores":{"a":1}}`},
		{ref, "i_1", `{"scores":{"b":2}}`},
		{ref, "i_12", `{"scores":{"c":3}}`},
		{other, "i_1", `{"scores":{"d":4}}`},
	} {
		err := store.AppendRanking(t.Context(), kept.ref, kept.instance, json.RawMessage(kept.record))
		if err != nil {
			t.Fatal(err)
		}
	}

	reopened, err := localadapter.NewFilesystemStagedBlobStoreAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rankingTexts(t, reopened, ref, "i_1"), []string{`{"scores":{"a":1}}`, `{"scores":{"b":2}}`}; !slices.Equal(got, want) {
		t.Fatalf("a store opened anew should read the instance's records and no other's, got %v", got)
	}
	if got := rankingTexts(t, reopened, ref, "i_3"); len(got) != 0 {
		t.Fatalf("an instance without records should read none, got %v", got)
	}

	err = reopened.DeleteStaged(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got := rankingTexts(t, store, ref, "i_1"); len(got) != 0 {
		t.Fatalf("the records should go with the session's staged area, got %v", got)
	}
	if got := rankingTexts(t, store, other, "i_1"); len(got) != 1 {
		t.Fatalf("another session's records should stay, got %v", got)
	}
}

func TestFilesystemStagedBlobStoreRefusesRankingNamesThatAreNoPlainSegment(t *testing.T) {
	store, err := localadapter.NewFilesystemStagedBlobStoreAt(canonicalTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	ref := sdd.SessionRef{Subject: "alice", Session: "s_one"}
	for _, instance := range []string{"", "..", "i/1", "i.1"} {
		t.Run(instance, func(t *testing.T) {
			err := store.AppendRanking(t.Context(), ref, instance, json.RawMessage(`{}`))
			if err == nil {
				t.Fatal("the instance should be refused as a file name part")
			}
			_, err = store.Rankings(t.Context(), ref, instance)
			if err == nil {
				t.Fatal("reading under the instance should be refused alike")
			}
		})
	}
}

package application_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// The JSON form of the ledger round-trips byte for byte: a compacted ledger
// exported and imported into an empty store yields the same segment, and
// bytes this sdd cannot name keep their number (D16).
func TestRelevanceJSON_RoundTripsByteForByte(t *testing.T) {
	pair := model.NewRelevancePair("20261101-120000-s-tac-aaa", "20261109-004506-d-cpt-udz")
	other := model.NewRelevancePair("20261101-120000-s-tac-aaa", "20261102-120000-d-tac-bbb")
	batches := []model.RelevanceBatch{
		{Readers: []model.ReaderRecord{{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 3, Audit: true}}}},
		{Readers: []model.ReaderRecord{{Pair: other, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced, Reason: 9, Relations: model.RelationOutdates | 1<<6}}}},
		{Scores: []model.ScoreRecord{
			{Pair: pair, Score: model.FilterScore{Judge: "systemone:jev-1.14.0", Direction: model.DirectionBoth, Score: 17}},
			{Pair: other, Score: model.FilterScore{Judge: "cohere-rerank:rerank-v3.5", Direction: model.DirectionBToA, Score: 230}},
		}},
		{Steps: []model.DriftStep{{Judge: "systemone:jev-1.14.0", At: time.Date(2026, 11, 9, 12, 0, 0, 500, time.UTC), SetAside: 40}}},
	}
	source, err := localadapter.NewFilesystemRelevanceStore(filepath.Join(t.TempDir(), "source"))
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range batches {
		if err := source.Append(t.Context(), relevance.Encode(batch)); err != nil {
			t.Fatal(err)
		}
	}
	if err := source.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	if err := sdd.ExportRelevanceJSON(t.Context(), source, &exported); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"reason":"reason-9"`, `"bits-64"`, `"audit":true`, `"direction":"b-to-a"`, `"type":"step"`} {
		if !strings.Contains(exported.String(), want) {
			t.Fatalf("export should carry %s:\n%s", want, exported.String())
		}
	}
	target, err := localadapter.NewFilesystemRelevanceStore(filepath.Join(t.TempDir(), "target"))
	if err != nil {
		t.Fatal(err)
	}
	count, err := sdd.ImportRelevanceJSON(t.Context(), target, bytes.NewReader(exported.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("import read %d records: every exported line should be a record", count)
	}
	want, err := source.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got, err := target.Segments(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 1 || len(got) != 1 || !bytes.Equal(got[0], want[0]) {
		t.Fatal("the imported segment should equal the compacted source segment byte for byte")
	}
	var again bytes.Buffer
	if err := sdd.ExportRelevanceJSON(t.Context(), target, &again); err != nil {
		t.Fatal(err)
	}
	if again.String() != exported.String() {
		t.Fatal("exporting the imported ledger should give the same JSON")
	}
}

func TestImportRelevanceJSON_RefusesAMalformedLine(t *testing.T) {
	for _, line := range []string{
		`{"type":"reader","a":"b","b":"a","code":"opened"}`,
		`{"type":"score","a":"a","b":"b","judge":"j","direction":"both"}`,
		`{"type":"verdict"}`,
		`{"type":"reader","a":"a","b":"b","code":"opened","extra":1}`,
	} {
		_, err := sdd.ImportRelevanceJSON(t.Context(), localadapter.NewMemoryRelevanceStore(), strings.NewReader(line+"\n"))
		if err == nil || !strings.Contains(err.Error(), "line 1") {
			t.Fatalf("%s: err = %v; want a refusal naming the line", line, err)
		}
	}
}

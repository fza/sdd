package relevance_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
	"github.com/networkteam/sdd/internal/segment"
)

func sampleBatch() model.RelevanceBatch {
	pair := model.NewRelevancePair("20261109-120000-d-cpt-new", "20261101-090000-s-tac-old")
	other := model.NewRelevancePair("20261109-120000-d-cpt-new", "20261102-090000-d-prc-oth")
	return model.RelevanceBatch{
		Readers: []model.ReaderRecord{
			{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderOpened, Reason: 3, Audit: true, Relations: model.RelationOutdates}},
			{Pair: other, Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: 200}},
		},
		Scores: []model.ScoreRecord{
			{Pair: pair, Score: model.FilterScore{Judge: "systemone:jev-1.14.0", Direction: model.DirectionBoth, Score: 255}},
			{Pair: other, Score: model.FilterScore{Judge: "cohere-rerank:rerank-v3.5", Direction: model.DirectionBToA, Score: 128}},
		},
		Steps: []model.DriftStep{{Judge: "systemone:jev-1.14.0", At: time.Date(2026, 11, 9, 10, 0, 0, 5, time.UTC), SetAside: 30}},
	}
}

func TestEncodeDecode_RoundTripsEveryRecord(t *testing.T) {
	batch := sampleBatch()
	data := relevance.Encode(batch)
	got, err := relevance.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, batch) {
		t.Fatalf("decoded\n%+v\nwant\n%+v", got, batch)
	}
	if !bytes.Equal(relevance.Encode(got), data) {
		t.Fatal("re-encoding a decoded segment changed its bytes")
	}
}

func TestDecode_KeepsReasonCodes(t *testing.T) {
	for _, code := range model.GroundingVerdictCodes() {
		reason, err := model.VerdictReasonByte(code)
		if err != nil {
			t.Fatal(err)
		}
		batch := model.RelevanceBatch{Readers: []model.ReaderRecord{{Pair: model.NewRelevancePair("20261109-120000-d-cpt-a", "20261109-120000-d-cpt-b"), Verdict: model.ReaderVerdict{Code: model.ReaderDismissed, Reason: reason}}}}
		got, err := relevance.Decode(relevance.Encode(batch))
		if err != nil {
			t.Fatal(err)
		}
		if model.VerdictReasonCode(got.Readers[0].Verdict.Reason) != code {
			t.Fatalf("code %s did not survive the round trip", code)
		}
	}
}

func TestDecode_RefusesCorruptRecords(t *testing.T) {
	tables := [][]byte{{0}, {0}}
	cases := map[string][][]byte{
		"no tables":             {{0}},
		"index past the table":  append(tables, []byte{'r', 5, 0, 1, 0, 0, 0}),
		"truncated reader":      {segment.AppendString(segment.AppendUvarint(segment.AppendUvarint(nil, 1), 0), "x"), {0}, {'r', 0, 0, 1}},
		"unknown record":        append(tables, []byte{'z'}),
		"shared past the prior": {segment.AppendString(segment.AppendUvarint(segment.AppendUvarint(nil, 1), 3), "x"), {0}},
		"oversized ID table":    {segment.AppendUvarint(nil, 1000), {0}},
		"bytes after a table":   {{0, 9}, {0}},
		"empty record":          append(tables, []byte{}),
	}
	for name, records := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := relevance.Decode(segment.Encode(relevance.Kind, records))
			if !errors.Is(err, segment.ErrCorrupt) {
				t.Fatalf("Decode = %v, want ErrCorrupt", err)
			}
		})
	}
}

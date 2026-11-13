package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
)

// SeedResult reports one seeding pass: the replayed labels it appended and
// the pairs it skipped because the ledger, or an earlier line, labels them.
type SeedResult struct {
	Added, Skipped int
}

type seedLine struct {
	A string `json:"a"`
	B string `json:"b"`
}

// SeedReplayLabels appends a referenced reader record of source replay for
// each pair a JSON Lines file names, as {"a": "<entry ID>", "b": "<entry
// ID>"} in either order, as one segment. A pair the ledger already labels,
// live or replayed, is skipped, so a second pass over one file adds nothing.
func SeedReplayLabels(ctx context.Context, store RelevanceStore, r io.Reader) (SeedResult, error) {
	segments, err := store.Segments(ctx)
	if err != nil {
		return SeedResult{}, err
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		return SeedResult{}, err
	}
	var batch model.RelevanceBatch
	var result SeedResult
	seen := map[model.RelevancePair]bool{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var record seedLine
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return SeedResult{}, fmt.Errorf("line %d: %w", line, err)
		}
		for _, id := range []string{record.A, record.B} {
			if _, err := model.ParseID(id); err != nil {
				return SeedResult{}, fmt.Errorf("line %d: %w", line, err)
			}
		}
		if record.A == record.B {
			return SeedResult{}, fmt.Errorf("line %d: a pair needs two entries, %s twice", line, record.A)
		}
		pair := model.NewRelevancePair(record.A, record.B)
		if _, labelled := ledger.Reader(pair); labelled || seen[pair] {
			result.Skipped++
			continue
		}
		seen[pair] = true
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{Code: model.ReaderReferenced, Source: model.SourceReplay}})
	}
	if err := scanner.Err(); err != nil {
		return SeedResult{}, err
	}
	if batch.Empty() {
		return result, nil
	}
	if err := store.Append(ctx, relevance.Encode(batch)); err != nil {
		return SeedResult{}, err
	}
	result.Added = len(batch.Readers)
	return result, nil
}

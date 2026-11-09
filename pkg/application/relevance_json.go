package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/relevance"
)

// relevanceJSON is one line of the JSON form of the relevance ledger
// (20261109-004506-d-cpt-udz, D16): a reader record, a score or a drift
// step. Bytes this sdd cannot name (an unknown reason) keep their number,
// so every record survives a round trip.
type relevanceJSON struct {
	Type      string   `json:"type"`
	A         string   `json:"a,omitempty"`
	B         string   `json:"b,omitempty"`
	Code      string   `json:"code,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Audit     bool     `json:"audit,omitempty"`
	Relations []string `json:"relations,omitempty"`
	Judge     string   `json:"judge,omitempty"`
	Direction string   `json:"direction,omitempty"`
	Score     *uint8   `json:"score,omitempty"`
	At        string   `json:"at,omitempty"`
	SetAside  *uint8   `json:"set_aside,omitempty"`
}

var relationNames = []struct {
	bit  uint8
	name string
}{{model.RelationContradicts, "contradicts"}, {model.RelationDuplicates, "duplicates"}, {model.RelationOutdates, "outdates"}}

// ExportRelevanceJSON writes the folded ledger as JSON Lines: reader
// records, then scores, then drift steps, each sorted, so equal ledgers
// export equal bytes.
func ExportRelevanceJSON(ctx context.Context, store RelevanceStore, w io.Writer) error {
	segments, err := store.Segments(ctx)
	if err != nil {
		return err
	}
	ledger, err := relevance.Fold(segments)
	if err != nil {
		return err
	}
	batch := ledger.Batch()
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, record := range batch.Readers {
		line := relevanceJSON{Type: "reader", A: record.Pair.A, B: record.Pair.B, Code: record.Verdict.Code.String(),
			Reason: model.VerdictReasonCode(record.Verdict.Reason), Audit: record.Verdict.Audit}
		for _, relation := range relationNames {
			if record.Verdict.Relations&relation.bit != 0 {
				line.Relations = append(line.Relations, relation.name)
			}
		}
		if rest := record.Verdict.Relations &^ (model.RelationContradicts | model.RelationDuplicates | model.RelationOutdates); rest != 0 {
			line.Relations = append(line.Relations, fmt.Sprintf("bits-%d", rest))
		}
		if err := encoder.Encode(line); err != nil {
			return err
		}
	}
	for _, record := range batch.Scores {
		score := record.Score.Score
		if err := encoder.Encode(relevanceJSON{Type: "score", A: record.Pair.A, B: record.Pair.B, Judge: record.Score.Judge, Direction: record.Score.Direction.String(), Score: &score}); err != nil {
			return err
		}
	}
	for _, step := range batch.Steps {
		setAside := step.SetAside
		if err := encoder.Encode(relevanceJSON{Type: "step", Judge: step.Judge, At: step.At.UTC().Format(time.RFC3339Nano), SetAside: &setAside}); err != nil {
			return err
		}
	}
	return nil
}

// ImportRelevanceJSON appends the records of a JSON Lines file, as export
// writes it, as one segment, and returns how many records it read.
func ImportRelevanceJSON(ctx context.Context, store RelevanceStore, r io.Reader) (int, error) {
	var batch model.RelevanceBatch
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var record relevanceJSON
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&record); err != nil {
			return 0, fmt.Errorf("line %d: %w", line, err)
		}
		if err := addRelevanceJSON(&batch, record); err != nil {
			return 0, fmt.Errorf("line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	count := len(batch.Readers) + len(batch.Scores) + len(batch.Steps)
	if count == 0 {
		return 0, nil
	}
	if err := store.Append(ctx, relevance.Encode(batch)); err != nil {
		return 0, err
	}
	return count, nil
}

func addRelevanceJSON(batch *model.RelevanceBatch, record relevanceJSON) error {
	pair := func() (model.RelevancePair, error) {
		if record.A == "" || record.B == "" || record.A >= record.B {
			return model.RelevancePair{}, errors.New("a pair needs entry IDs a and b, a sorting before b")
		}
		return model.RelevancePair{A: record.A, B: record.B}, nil
	}
	switch record.Type {
	case "reader":
		p, err := pair()
		if err != nil {
			return err
		}
		code, err := model.ParseReaderCode(record.Code)
		if err != nil {
			return err
		}
		reason, err := model.ParseVerdictReason(record.Reason)
		if err != nil {
			return err
		}
		var relations uint8
		for _, name := range record.Relations {
			bit, err := parseRelation(name)
			if err != nil {
				return err
			}
			relations |= bit
		}
		batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: p, Verdict: model.ReaderVerdict{Code: code, Reason: reason, Audit: record.Audit, Relations: relations}})
	case "score":
		p, err := pair()
		if err != nil {
			return err
		}
		direction, err := model.ParseRelevanceDirection(record.Direction)
		if err != nil {
			return err
		}
		if record.Judge == "" || record.Score == nil {
			return errors.New("a score needs a judge and a score")
		}
		batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: p, Score: model.FilterScore{Judge: record.Judge, Direction: direction, Score: *record.Score}})
	case "step":
		at, err := time.Parse(time.RFC3339Nano, record.At)
		if err != nil {
			return fmt.Errorf("step time: %w", err)
		}
		if record.Judge == "" || record.SetAside == nil {
			return errors.New("a step needs a judge and a set-aside threshold")
		}
		batch.Steps = append(batch.Steps, model.DriftStep{Judge: record.Judge, At: at.UTC(), SetAside: *record.SetAside})
	default:
		return fmt.Errorf("unknown record type %q (reader, score, step)", record.Type)
	}
	return nil
}

func parseRelation(name string) (uint8, error) {
	for _, relation := range relationNames {
		if relation.name == name {
			return relation.bit, nil
		}
	}
	var bits uint8
	if _, err := fmt.Sscanf(name, "bits-%d", &bits); err == nil && bits > 0 {
		return bits, nil
	}
	return 0, fmt.Errorf("unknown relation %q (contradicts, duplicates, outdates)", name)
}

// Package relevance is the binary codec of the relevance verdict ledger
// (20261109-004506-d-cpt-udz, 5.7): one segment per write, in the shared
// segment format. A segment's first record is its entry ID table, sorted and
// front-coded; its second the judge table; every later record names its
// entries and judge by index:
//
//	reader  'r' a b code reason flags relations
//	score   's' a b judge direction score
//	step    'd' judge time set-aside
//
// Indexes and times are varints; code, reason, flags, relations, direction
// and scores one byte each. A reason byte this sdd does not know is kept as
// it is (D10).
package relevance

import (
	"fmt"
	"slices"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/segment"
)

// Kind is the segment kind of a relevance ledger segment.
const Kind = "sdd.relevance"

const (
	recordReader = 'r'
	recordScore  = 's'
	recordStep   = 'd'
	flagAudit    = 1
)

// Encode returns the segment bytes of batch.
func Encode(batch model.RelevanceBatch) []byte {
	ids := map[string]bool{}
	judges := map[string]bool{}
	for _, record := range batch.Readers {
		ids[record.Pair.A], ids[record.Pair.B] = true, true
	}
	for _, record := range batch.Scores {
		ids[record.Pair.A], ids[record.Pair.B] = true, true
		judges[record.Score.Judge] = true
	}
	for _, step := range batch.Steps {
		judges[step.Judge] = true
	}
	idTable := sortedSet(ids)
	judgeTable := sortedSet(judges)
	idIndex := indexOf(idTable)
	judgeIndex := indexOf(judgeTable)

	records := [][]byte{frontCode(idTable), stringTable(judgeTable)}
	for _, record := range batch.Readers {
		out := []byte{recordReader}
		out = segment.AppendUvarint(out, idIndex[record.Pair.A])
		out = segment.AppendUvarint(out, idIndex[record.Pair.B])
		var flags byte
		if record.Verdict.Audit {
			flags |= flagAudit
		}
		records = append(records, append(out, byte(record.Verdict.Code), record.Verdict.Reason, flags, record.Verdict.Relations))
	}
	for _, record := range batch.Scores {
		out := []byte{recordScore}
		out = segment.AppendUvarint(out, idIndex[record.Pair.A])
		out = segment.AppendUvarint(out, idIndex[record.Pair.B])
		out = segment.AppendUvarint(out, judgeIndex[record.Score.Judge])
		records = append(records, append(out, byte(record.Score.Direction), record.Score.Score))
	}
	for _, step := range batch.Steps {
		out := []byte{recordStep}
		out = segment.AppendUvarint(out, judgeIndex[step.Judge])
		out = segment.AppendVarint(out, step.At.UnixNano())
		records = append(records, append(out, step.SetAside))
	}
	return segment.Encode(Kind, records)
}

// Decode returns the batch a segment holds, records in the order written.
func Decode(data []byte) (model.RelevanceBatch, error) {
	records, err := segment.Decode(data, Kind)
	if err != nil {
		return model.RelevanceBatch{}, err
	}
	if len(records) < 2 {
		return model.RelevanceBatch{}, fmt.Errorf("%w: relevance segment without its tables", segment.ErrCorrupt)
	}
	ids, err := readFrontCoded(records[0])
	if err != nil {
		return model.RelevanceBatch{}, err
	}
	judges, err := readStringTable(records[1])
	if err != nil {
		return model.RelevanceBatch{}, err
	}
	var batch model.RelevanceBatch
	for _, raw := range records[2:] {
		if len(raw) == 0 {
			return model.RelevanceBatch{}, fmt.Errorf("%w: empty relevance record", segment.ErrCorrupt)
		}
		reader := segment.NewReader(raw[1:])
		switch raw[0] {
		case recordReader:
			pair, err := readPair(reader, ids)
			if err != nil {
				return model.RelevanceBatch{}, err
			}
			tail, err := fixed(reader, 4)
			if err != nil {
				return model.RelevanceBatch{}, err
			}
			batch.Readers = append(batch.Readers, model.ReaderRecord{Pair: pair, Verdict: model.ReaderVerdict{
				Code: model.ReaderCode(tail[0]), Reason: tail[1], Audit: tail[2]&flagAudit != 0, Relations: tail[3],
			}})
		case recordScore:
			pair, err := readPair(reader, ids)
			if err != nil {
				return model.RelevanceBatch{}, err
			}
			judge, err := readIndex(reader, judges)
			if err != nil {
				return model.RelevanceBatch{}, err
			}
			tail, err := fixed(reader, 2)
			if err != nil {
				return model.RelevanceBatch{}, err
			}
			batch.Scores = append(batch.Scores, model.ScoreRecord{Pair: pair, Score: model.FilterScore{
				Judge: judge, Direction: model.RelevanceDirection(tail[0]), Score: tail[1],
			}})
		case recordStep:
			judge, err := readIndex(reader, judges)
			if err != nil {
				return model.RelevanceBatch{}, err
			}
			nanos, err := reader.Varint()
			if err != nil {
				return model.RelevanceBatch{}, err
			}
			tail, err := fixed(reader, 1)
			if err != nil {
				return model.RelevanceBatch{}, err
			}
			batch.Steps = append(batch.Steps, model.DriftStep{Judge: judge, At: time.Unix(0, nanos).UTC(), SetAside: tail[0]})
		default:
			return model.RelevanceBatch{}, fmt.Errorf("%w: unknown relevance record type %q", segment.ErrCorrupt, raw[0])
		}
	}
	return batch, nil
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func indexOf(table []string) map[string]uint64 {
	index := make(map[string]uint64, len(table))
	for i, value := range table {
		index[value] = uint64(i)
	}
	return index
}

// frontCode writes sorted IDs as the length each shares with the one before
// it and the rest: entry IDs share their date prefix, so most cost a few
// bytes.
func frontCode(sorted []string) []byte {
	out := segment.AppendUvarint(nil, uint64(len(sorted)))
	previous := ""
	for _, id := range sorted {
		shared := 0
		for shared < len(previous) && shared < len(id) && previous[shared] == id[shared] {
			shared++
		}
		out = segment.AppendUvarint(out, uint64(shared))
		out = segment.AppendString(out, id[shared:])
		previous = id
	}
	return out
}

func readFrontCoded(raw []byte) ([]string, error) {
	reader := segment.NewReader(raw)
	count, err := reader.Uvarint()
	if err != nil {
		return nil, err
	}
	if count > uint64(len(raw)) {
		return nil, fmt.Errorf("%w: ID table of %d entries in %d bytes", segment.ErrCorrupt, count, len(raw))
	}
	ids := make([]string, 0, count)
	previous := ""
	for range count {
		shared, err := reader.Uvarint()
		if err != nil {
			return nil, err
		}
		rest, err := reader.String()
		if err != nil {
			return nil, err
		}
		if shared > uint64(len(previous)) {
			return nil, fmt.Errorf("%w: ID shares %d bytes of a %d-byte predecessor", segment.ErrCorrupt, shared, len(previous))
		}
		id := previous[:shared] + rest
		ids = append(ids, id)
		previous = id
	}
	if !reader.Empty() {
		return nil, fmt.Errorf("%w: bytes after the ID table", segment.ErrCorrupt)
	}
	return ids, nil
}

func stringTable(values []string) []byte {
	out := segment.AppendUvarint(nil, uint64(len(values)))
	for _, value := range values {
		out = segment.AppendString(out, value)
	}
	return out
}

func readStringTable(raw []byte) ([]string, error) {
	reader := segment.NewReader(raw)
	count, err := reader.Uvarint()
	if err != nil {
		return nil, err
	}
	if count > uint64(len(raw)) {
		return nil, fmt.Errorf("%w: judge table of %d entries in %d bytes", segment.ErrCorrupt, count, len(raw))
	}
	values := make([]string, 0, count)
	for range count {
		value, err := reader.String()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if !reader.Empty() {
		return nil, fmt.Errorf("%w: bytes after the judge table", segment.ErrCorrupt)
	}
	return values, nil
}

func readPair(reader *segment.Reader, ids []string) (model.RelevancePair, error) {
	a, err := readIndex(reader, ids)
	if err != nil {
		return model.RelevancePair{}, err
	}
	b, err := readIndex(reader, ids)
	if err != nil {
		return model.RelevancePair{}, err
	}
	return model.RelevancePair{A: a, B: b}, nil
}

func readIndex(reader *segment.Reader, table []string) (string, error) {
	index, err := reader.Uvarint()
	if err != nil {
		return "", err
	}
	if index >= uint64(len(table)) {
		return "", fmt.Errorf("%w: index %d outside a table of %d", segment.ErrCorrupt, index, len(table))
	}
	return table[index], nil
}

func fixed(reader *segment.Reader, n int) ([]byte, error) {
	out := make([]byte, n)
	for i := range out {
		value, err := reader.Byte()
		if err != nil {
			return nil, err
		}
		out[i] = value
	}
	if !reader.Empty() {
		return nil, fmt.Errorf("%w: bytes after the relevance record", segment.ErrCorrupt)
	}
	return out, nil
}

// Fold decodes segments and folds them into one ledger.
func Fold(segments [][]byte) (*model.RelevanceLedger, error) {
	ledger := model.NewRelevanceLedger()
	for i, data := range segments {
		batch, err := Decode(data)
		if err != nil {
			return nil, fmt.Errorf("segment %d: %w", i, err)
		}
		ledger.Add(batch)
	}
	return ledger, nil
}

package relevance

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/segment"
)

// ProofsKind is the segment kind of the proof records that gate the hiding
// rules (model.ProofRecords). Each record is a type byte and a JSON body:
//
//	replay  'p' model.ReplayCheck
//	tally   't' model.CheckTally
//	scanned 'c' model.Scanned
//	miss    'm' model.Miss
//
// A record of a type this build does not know is skipped, so a later build
// may add types.
const ProofsKind = "sdd.relevance.proofs"

const (
	proofReplay  = 'p'
	proofTally   = 't'
	proofScanned = 'c'
	proofMiss    = 'm'
)

// EncodeProofs returns the segment bytes of records.
func EncodeProofs(records model.ProofRecords) ([]byte, error) {
	var raw [][]byte
	add := func(kind byte, body any) error {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding a proof record: %w", err)
		}
		raw = append(raw, append([]byte{kind}, data...))
		return nil
	}
	for _, replay := range records.Replays {
		if err := add(proofReplay, replay); err != nil {
			return nil, err
		}
	}
	for _, tally := range records.Tallies {
		if err := add(proofTally, tally); err != nil {
			return nil, err
		}
	}
	for _, scanned := range records.Scans {
		if err := add(proofScanned, scanned); err != nil {
			return nil, err
		}
	}
	for _, miss := range records.Misses {
		if err := add(proofMiss, miss); err != nil {
			return nil, err
		}
	}
	return segment.Encode(ProofsKind, raw), nil
}

// FoldProofs decodes segments into one set of records, each distinct record
// once, so segments read in any order and with duplicates fold alike.
func FoldProofs(segments [][]byte) (model.ProofRecords, error) {
	var records model.ProofRecords
	seen := map[string]bool{}
	for i, data := range segments {
		raw, err := segment.Decode(data, ProofsKind)
		if err != nil {
			return model.ProofRecords{}, fmt.Errorf("proof segment %d: %w", i, err)
		}
		for _, record := range raw {
			if len(record) == 0 {
				return model.ProofRecords{}, fmt.Errorf("proof segment %d: %w: empty record", i, segment.ErrCorrupt)
			}
			if seen[string(record)] {
				continue
			}
			seen[string(record)] = true
			err := decodeProof(record, &records)
			if err != nil {
				return model.ProofRecords{}, fmt.Errorf("proof segment %d: %w", i, err)
			}
		}
	}
	slices.SortStableFunc(records.Replays, func(a, b model.ReplayCheck) int { return a.At.Compare(b.At) })
	slices.SortStableFunc(records.Tallies, func(a, b model.CheckTally) int { return a.At.Compare(b.At) })
	slices.SortStableFunc(records.Misses, func(a, b model.Miss) int { return a.At.Compare(b.At) })
	return records, nil
}

func decodeProof(record []byte, records *model.ProofRecords) error {
	body := record[1:]
	var err error
	switch record[0] {
	case proofReplay:
		var replay model.ReplayCheck
		err = json.Unmarshal(body, &replay)
		records.Replays = append(records.Replays, replay)
	case proofTally:
		var tally model.CheckTally
		err = json.Unmarshal(body, &tally)
		records.Tallies = append(records.Tallies, tally)
	case proofScanned:
		var scanned model.Scanned
		err = json.Unmarshal(body, &scanned)
		records.Scans = append(records.Scans, scanned)
	case proofMiss:
		var miss model.Miss
		err = json.Unmarshal(body, &miss)
		records.Misses = append(records.Misses, miss)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", segment.ErrCorrupt, err)
	}
	return nil
}

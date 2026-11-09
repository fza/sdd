package local

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Tiered compaction of a segment directory (20261109-004506-d-cpt-udz,
// D17): a segment's tier is its size in powers of four above 4 KiB, and
// once a tier holds compactFanIn segments they merge into one, which lands
// in a higher tier. Merging writes the new segment before it removes the
// old ones, so a reader sees every record at least once; two compactions
// racing leave duplicate records, which the next merge drops.
const (
	compactFanIn    = 8
	compactTierBase = 4096
	maxCompactions  = 16
)

func segmentTier(size int) int {
	tier := 0
	for scaled := size / compactTierBase; scaled >= 4; scaled /= 4 {
		tier++
	}
	return tier
}

// tierToMerge returns the segments of the lowest tier holding compactFanIn
// or more, none when no tier does.
func tierToMerge(files []segmentFile) []segmentFile {
	byTier := map[int][]segmentFile{}
	for _, file := range files {
		tier := segmentTier(len(file.data))
		byTier[tier] = append(byTier[tier], file)
	}
	tiers := make([]int, 0, len(byTier))
	for tier := range byTier {
		tiers = append(tiers, tier)
	}
	slices.Sort(tiers)
	for _, tier := range tiers {
		if len(byTier[tier]) >= compactFanIn {
			return byTier[tier]
		}
	}
	return nil
}

// segmentMerge merges segments into one; nil when no record is left.
type segmentMerge func(segments [][]byte) ([]byte, error)

// compactDir merges segments of dir: every segment when full, else tier by
// tier while a tier overflows.
func compactDir(ctx context.Context, dir string, full bool, merge segmentMerge) error {
	for range maxCompactions {
		files, err := readSegmentFiles(ctx, dir)
		if err != nil {
			return err
		}
		group := tierToMerge(files)
		if full {
			group = files
		}
		if len(group) == 0 {
			return nil
		}
		data := make([][]byte, len(group))
		for i, file := range group {
			data[i] = file.data
		}
		merged, err := merge(data)
		if err != nil {
			return err
		}
		if len(group) == 1 && bytes.Equal(merged, group[0].data) {
			return nil
		}
		if merged != nil {
			err = writeSegmentFile(dir, merged)
			if err != nil {
				return err
			}
		}
		for _, file := range group {
			err := os.Remove(filepath.Join(dir, file.name))
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if full {
			return nil
		}
	}
	return nil
}

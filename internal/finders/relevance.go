package finders

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// LeftoverRelevance lists the relevance segments left under the project's
// .sdd/relevance/.
func (f *Finder) LeftoverRelevance(q query.LeftoverRelevanceQuery) (*query.LeftoverRelevanceResult, error) {
	result := &query.LeftoverRelevanceResult{}
	if q.SDDDir == "" {
		return result, nil
	}
	result.Dir = filepath.Join(q.SDDDir, model.RelevanceDirName)
	files, err := os.ReadDir(result.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if file.Type().IsRegular() && strings.HasSuffix(file.Name(), ".seg") {
			result.Segments = append(result.Segments, filepath.Join(result.Dir, file.Name()))
		}
	}
	return result, nil
}

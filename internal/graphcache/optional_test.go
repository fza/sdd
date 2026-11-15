package graphcache_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/filestamp"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/graphcache"
)

type failingCache struct{}

func (failingCache) Load(string, map[string]filestamp.Stamp) (finders.GraphDocuments, bool, error) {
	return finders.GraphDocuments{}, false, errors.New("permission denied")
}

func (failingCache) Save(string, map[string]filestamp.Stamp, finders.GraphDocuments) error {
	return errors.New("no space left on device")
}

func TestOptionalCountsAFailureAsAMissLoggedOnce(t *testing.T) {
	var logs bytes.Buffer
	cache := graphcache.NewOptional(failingCache{}, slog.New(slog.NewTextHandler(&logs, nil)))
	if _, hit, err := cache.Load("dir", nil); err != nil || hit {
		t.Errorf("a failed load should be a miss, got hit %v, err %v", hit, err)
	}
	if err := cache.Save("dir", nil, finders.GraphDocuments{}); err != nil {
		t.Errorf("a failed save should not fail the read, got %v", err)
	}
	if n := strings.Count(logs.String(), "graph cache unavailable"); n != 1 {
		t.Errorf("the failures should be logged once, logged:\n%s", logs.String())
	}
}

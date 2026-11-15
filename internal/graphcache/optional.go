package graphcache

import (
	"log/slog"
	"sync"

	"github.com/networkteam/sdd/internal/filestamp"
	"github.com/networkteam/sdd/internal/finders"
)

// Optional is a finders.GraphCache a graph read never fails on: a load, save
// or use stamp that fails, as on a cache dir sdd cannot write, counts as a
// miss, logged once. The read then comes complete from the entry files.
type Optional struct {
	cache  finders.GraphCache
	log    *slog.Logger
	logged sync.Once
}

// NewOptional wraps cache, logging its first failure to log.
func NewOptional(cache finders.GraphCache, log *slog.Logger) *Optional {
	return &Optional{cache: cache, log: log}
}

func (o *Optional) Load(dir string, stamps map[string]filestamp.Stamp) (finders.GraphDocuments, bool, error) {
	docs, hit, err := o.cache.Load(dir, stamps)
	if err != nil {
		o.failed(err)
		return finders.GraphDocuments{}, false, nil
	}
	return docs, hit, nil
}

func (o *Optional) Save(dir string, stamps map[string]filestamp.Stamp, docs finders.GraphDocuments) error {
	if err := o.cache.Save(dir, stamps, docs); err != nil {
		o.failed(err)
	}
	return nil
}

func (o *Optional) failed(err error) {
	o.logged.Do(func() {
		o.log.Warn("graph cache unavailable, reading the entry files; SDD_GRAPH_CACHE=off skips it", "err", err)
	})
}

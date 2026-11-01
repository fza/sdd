package local

import (
	"context"
	"fmt"

	"github.com/networkteam/sdd/internal/index"
	app "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

func (s *PersistentSearchIndexStore) EntryPublished(ctx context.Context, version app.SearchEntryVersion) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	dir, err := s.storeDir(version.Namespace)
	if err != nil {
		return false, err
	}
	manifest, err := s.readManifest(dir)
	if err != nil {
		return false, err
	}
	return publishedVersion(manifest, version), nil
}

func publishedVersion(manifest *index.Manifest, version app.SearchEntryVersion) bool {
	state := manifest.Entries[version.EntryID]
	for _, stored := range state.Versions {
		if stored.Hash == version.EntryHash && stored.Fingerprint == version.Namespace.Fingerprint {
			return true
		}
	}
	return false
}

func (s *PersistentSearchIndexStore) PublishEntry(ctx context.Context, version app.SearchEntryVersion, chunks []app.IndexedChunk) error {
	return s.PublishEntries(ctx, []types.SearchEntryPublication{{Version: version, Chunks: chunks}})
}

// PublishEntries publishes every listed version in one write session per
// store directory: one store load, one chunk-ownership pass and one manifest
// save cover the whole batch, so a reindex costs a store load per batch
// instead of per entry (20261101-003415-s-tac-1fw). The manifest save commits
// the batch; a failure before it publishes none of the batch.
func (s *PersistentSearchIndexStore) PublishEntries(ctx context.Context, publications []types.SearchEntryPublication) error {
	var dirs []string
	byDir := map[string][]types.SearchEntryPublication{}
	for _, publication := range publications {
		if err := types.ValidateEntryPublication(publication.Version, publication.Chunks); err != nil {
			return err
		}
		dir, err := s.storeDir(publication.Version.Namespace)
		if err != nil {
			return err
		}
		if byDir[dir] == nil {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], publication)
	}
	for _, dir := range dirs {
		if err := s.publishInto(ctx, dir, byDir[dir]); err != nil {
			return err
		}
		if err := refreshSegments(ctx, dir); err != nil {
			return err
		}
	}
	return nil
}

type publishedVersionKey struct{ entryID, entryHash string }

func (s *PersistentSearchIndexStore) publishInto(ctx context.Context, dir string, publications []types.SearchEntryPublication) error {
	return index.WriteStore(ctx, dir, func(idx *index.Index) error {
		manifest, err := index.LoadManifest(dir)
		if err != nil {
			return err
		}
		owners := chunkOwners(manifest)
		var upserts []index.EntryUpsert
		for _, publication := range publications {
			version := publication.Version
			if publishedVersion(manifest, version) {
				continue
			}
			key := publishedVersionKey{version.EntryID, version.EntryHash}
			rows := make([]index.Row, len(publication.Chunks))
			ids := make([]string, len(publication.Chunks))
			for i, chunk := range publication.Chunks {
				if owner, ok := owners[chunk.Chunk.ID]; ok && owner != key {
					return fmt.Errorf("sdd: chunk ID conflicts with another published version")
				}
				rows[i] = rowFromChunk(chunk, version.Namespace.Fingerprint)
				ids[i] = chunk.Chunk.ID
			}
			for _, id := range ids {
				owners[id] = key
			}
			upserts = append(upserts, index.EntryUpsert{EntryID: version.EntryID, Rows: rows})
			manifest.AddVersion(version.EntryID, index.EntryVersion{Hash: version.EntryHash, Fingerprint: version.Namespace.Fingerprint, ChunkIDs: ids, IndexedAt: s.now()})
		}
		if len(upserts) == 0 {
			return nil
		}
		if err := idx.UpsertEntries(ctx, upserts); err != nil {
			return err
		}
		return manifest.Save(dir)
	})
}

// chunkOwners maps each stored chunk ID to the version holding it. A chunk ID
// two versions hold maps to the zero key, which no publication matches, so
// publishing either version again reports the conflict.
func chunkOwners(manifest *index.Manifest) map[string]publishedVersionKey {
	owners := map[string]publishedVersionKey{}
	for entryID, state := range manifest.Entries {
		for _, stored := range state.Versions {
			key := publishedVersionKey{entryID, stored.Hash}
			for _, id := range stored.ChunkIDs {
				if owner, ok := owners[id]; ok && owner != key {
					owners[id] = publishedVersionKey{}
					continue
				}
				owners[id] = key
			}
		}
	}
	return owners
}

func (s *PersistentSearchIndexStore) readManifest(dir string) (*index.Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache := s.manifests[dir]
	if cache == nil {
		cache = &index.ManifestCache{}
		s.manifests[dir] = cache
	}
	return cache.Read(dir)
}

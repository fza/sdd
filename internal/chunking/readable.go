package chunking

import (
	"context"

	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/model"
)

// ReadableEntryHash is the state hash of the version of entry a search reads
// from a store read under caps, newest first (model.ReadableVersion).
// published reports whether the store holds a version of the entry; it is
// asked only while caps hold more than one cap.
func ReadableEntryHash(ctx context.Context, entry *model.Entry, attachments AttachmentReader, caps []int, published func(hash string) (bool, error)) (string, error) {
	candidates := make([]string, 0, len(caps))
	for _, maxRunes := range caps {
		hash, err := EntryStateHashCapped(ctx, entry, attachments, maxRunes)
		if err != nil {
			return "", err
		}
		candidates = append(candidates, hash)
	}
	stored := map[string]bool{}
	if len(candidates) > 1 {
		for _, hash := range candidates {
			ok, err := published(hash)
			if err != nil {
				return "", err
			}
			stored[hash] = ok
		}
	}
	return model.ReadableVersion(candidates, stored), nil
}

// ReadableEntryHashes maps each indexable entry to the version a search of the
// store manifest describes reads (ReadableEntryHash).
func ReadableEntryHashes(ctx context.Context, entries []*model.Entry, attachments AttachmentReader, manifest *index.Manifest, excludeEmbedded bool) (map[string]string, error) {
	hashes := make(map[string]string, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !IncludeEntry(entry, excludeEmbedded) {
			continue
		}
		hash, err := ReadableManifestHash(ctx, entry, attachments, manifest)
		if err != nil {
			return nil, err
		}
		hashes[entry.ID] = hash
	}
	return hashes, nil
}

// ReadableManifestHash is ReadableEntryHash for the store manifest describes.
func ReadableManifestHash(ctx context.Context, entry *model.Entry, attachments AttachmentReader, manifest *index.Manifest) (string, error) {
	return ReadableEntryHash(ctx, entry, attachments, manifest.ReadMaxRunes(), func(hash string) (bool, error) {
		return manifest.HasHash(entry.ID, hash), nil
	})
}

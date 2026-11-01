package handlers

import (
	"context"
	"errors"
	"fmt"
	"iter"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/chunking"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/textsplitter"
	"github.com/networkteam/sdd/pkg/application/types"
	"github.com/networkteam/sdd/pkg/llm/embed"
)

type indexInput struct {
	entry   *model.Entry
	version types.SearchEntryVersion
}

func indexBatchSize(emb embed.Embedder) int {
	if local, ok := emb.(IndexEmbedder); ok && local.BatchSize > 0 {
		return local.BatchSize
	}
	return 32
}

// eachEntry adapts a per-entry publisher to indexStream's per-batch publish.
func eachEntry(publish func(context.Context, *model.IndexWork) error) func(context.Context, []*model.IndexWork) error {
	return func(ctx context.Context, entries []*model.IndexWork) error {
		for _, entry := range entries {
			if err := publish(ctx, entry); err != nil {
				return err
			}
		}
		return nil
	}
}

func prepareIndexEntry(ctx context.Context, input indexInput, reader chunking.AttachmentReader, splitter *textsplitter.Splitter, skip func(types.CanonicalChunk) bool) (*model.IndexWork, error) {
	attachments := &chunking.CachedAttachments{Reader: reader}
	hash, err := chunking.EntryStateHashCapped(ctx, input.entry, attachments, splitter.MaxRunes())
	if err != nil {
		return nil, err
	}
	if input.version.EntryHash != "" && hash != input.version.EntryHash {
		return nil, fmt.Errorf("sdd: pinned entry content does not match descriptor")
	}
	input.version.EntryHash = hash
	chunks, err := chunking.DeriveChunks(ctx, input.entry, hash, splitter, attachments)
	if err != nil {
		return nil, err
	}
	prepared := &model.IndexWork{Version: input.version}
	for _, chunk := range chunks {
		canonical := chunking.CanonicalChunk(input.entry.ID, hash, chunk)
		if skip != nil && skip(canonical) {
			continue
		}
		indexable, reason, err := secrets.IndexableChunk(canonical.Text)
		if err != nil {
			return nil, err
		}
		if !indexable {
			slogutils.FromContext(ctx).Warn("kept a chunk out of the vector index", "entry", input.entry.ID, "reason", reason)
			continue
		}
		prepared.Rows = append(prepared.Rows, types.IndexedChunk{Chunk: canonical})
	}
	return prepared, nil
}

// entriesPerPublication bounds how many finished entries wait for one
// publication. A store write reloads the whole store, so publishing per entry
// makes a reindex quadratic (20261101-003415-s-tac-1fw); an interrupted run
// redoes at most this many entries' embeddings.
const entriesPerPublication = 64

// indexStream derives, embeds and publishes the inputs. publish receives
// finished entries in batches of up to entriesPerPublication, in input order;
// entries finished before a failure are still published, and the failure is
// returned beside any publication error.
func indexStream(ctx context.Context, inputs iter.Seq2[indexInput, error], emb embed.Embedder, reader chunking.AttachmentReader, splitter *textsplitter.Splitter, skip func(types.CanonicalChunk) bool, publish func(context.Context, []*model.IndexWork) error, onBatch func([]string, int)) error {
	var pending []*model.IndexWork
	flush := func(ctx context.Context) error {
		if len(pending) == 0 {
			return nil
		}
		batch := pending
		pending = nil
		return publish(ctx, batch)
	}
	finish := func(ctx context.Context, entries []*model.IndexWork) error {
		pending = append(pending, entries...)
		for len(pending) >= entriesPerPublication {
			batch := pending[:entriesPerPublication:entriesPerPublication]
			pending = pending[entriesPerPublication:]
			if err := publish(ctx, batch); err != nil {
				return err
			}
		}
		return nil
	}
	err := embedStream(ctx, inputs, emb, reader, splitter, skip, finish, onBatch)
	return errors.Join(err, flush(ctx))
}

func embedStream(ctx context.Context, inputs iter.Seq2[indexInput, error], emb embed.Embedder, reader chunking.AttachmentReader, splitter *textsplitter.Splitter, skip func(types.CanonicalChunk) bool, finish func(context.Context, []*model.IndexWork) error, onBatch func([]string, int)) error {
	items := func(yield func(model.IndexItem, error) bool) {
		for input, err := range inputs {
			if err == nil {
				err = ctx.Err()
			}
			if err != nil {
				yield(model.IndexItem{}, err)
				return
			}
			entry, err := prepareIndexEntry(ctx, input, reader, splitter, skip)
			if err != nil {
				yield(model.IndexItem{}, err)
				return
			}
			if len(entry.Rows) == 0 {
				if err := finish(ctx, []*model.IndexWork{entry}); err != nil {
					yield(model.IndexItem{}, err)
					return
				}
			}
			for i := range entry.Rows {
				if !yield(model.IndexItem{Owner: entry, Position: i}, nil) {
					return
				}
			}
		}
	}
	for batch, err := range model.Batches(items, indexBatchSize(emb)) {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		work := model.IndexBatch(batch)
		if onBatch != nil {
			onBatch(work.EntryIDs(), len(batch))
		}
		result, err := emb.Embed(ctx, embed.Request{Purpose: embed.PurposeDocument, Texts: work.Texts()})
		if err != nil {
			return err
		}
		completed, err := work.Complete(result.Vectors)
		if err != nil {
			return err
		}
		for _, entry := range completed {
			if err := types.ValidateEntryPublication(entry.Version, entry.Rows); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(completed) > 0 {
			if err := finish(ctx, completed); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

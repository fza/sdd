package cliapp

import (
	"context"
	"io"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
)

// furtherReadFinder is newReadFinder whose graphs resolve the refs a direct
// dependency holds into repos beyond the direct dependencies
// (20261113-235859-d-cpt-l3m). settle waits for a fetch on a miss the read
// stopped waiting for; call it once the read is out.
func furtherReadFinder(ctx context.Context, errWriter io.Writer) (f *finders.Finder, settle func(), err error) {
	f, err = newReadFinder()
	if err != nil {
		return nil, func() {}, err
	}
	h, err := repoHandler(errWriter)
	if err != nil {
		return nil, func() {}, err
	}
	return f.WithFurther(furtherRefs(ctx, f, h)), h.Settle, nil
}

// furtherRefs resolves an entry of a repo beyond the direct dependencies from
// that repo's clone as it stands, else through the handler's fetch on a miss.
func furtherRefs(ctx context.Context, clones *finders.Finder, h *handlers.Handler) model.FurtherResolver {
	return func(repoID model.RepoID, entryID string, fetched bool) (model.FurtherRef, error) {
		g, at, err := clones.FurtherClone(repoID)
		if err != nil || (g != nil && g.ByID[entryID] != nil) {
			return model.FurtherRef{Graph: g, CloneFetched: at}, err
		}
		var ref model.FurtherRef
		err = h.ResolveFurtherRef(ctx, &command.ResolveFurtherRefCmd{
			RepoID: repoID, EntryID: entryID, Fetched: fetched,
			OnFetched:     func() { ref.Fetched = true },
			OnUnavailable: func(u model.UnavailableRef) { ref.Unavailable = &u },
		})
		if err != nil {
			return ref, err
		}
		ref.Graph, ref.CloneFetched, err = clones.FurtherClone(repoID)
		return ref, err
	}
}

// warnUnavailableRefs reports each ref beyond the direct dependencies the
// read over g could not resolve as one typed warning.
func warnUnavailableRefs(ctx context.Context, g *model.Graph) {
	logger := slogutils.FromContext(ctx)
	for _, u := range g.UnavailableRefs() {
		logger.Warn("dependency ref unavailable: "+u.Reason(), "kind", u.Kind, "ref", model.CrossRepoID(u.Repo, u.EntryID), "who", u.Actor(), "at", u.At)
	}
}

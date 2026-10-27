package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
)

// MigrateSchema stamps the graph with the binary's schema version and commits
// meta.json. Stamping comes before any backfill the migration owes, so writes
// resume at once and binaries that would drop the new fields are locked out
// while the backfill runs. A graph already at the version is left alone; a
// newer one is refused.
func (h *Handler) MigrateSchema(ctx context.Context, cmd *command.MigrateSchemaCmd) error {
	if cmd.SDDDir == "" {
		return fmt.Errorf("SDDDir is required")
	}
	path := filepath.Join(cmd.SDDDir, model.SchemaMetaFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	meta, err := model.ParseSchemaMeta(data)
	if err != nil {
		return err
	}
	from := meta.GraphSchemaVersion
	if from > cmd.SchemaVersion {
		return fmt.Errorf("graph schema version %d is newer than this binary's schema version %d; upgrade sdd to continue", from, cmd.SchemaVersion)
	}
	if from == cmd.SchemaVersion {
		return nil
	}

	meta.GraphSchemaVersion = cmd.SchemaVersion
	out, err := model.FormatSchemaMeta(*meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if h.committer != nil {
		if err := h.commitGraph(ctx, fmt.Sprintf("sdd: migrate graph schema %d -> %d", from, cmd.SchemaVersion), path); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
	}
	if cmd.OnStamped != nil {
		cmd.OnStamped(from, cmd.SchemaVersion)
	}
	return nil
}

// CommitStrandedTitles commits entry files that hold a title on disk but not
// at HEAD. A backfill whose commit failed leaves exactly those behind, and a
// rerun skips entries that already carry a title, so nothing else would ever
// commit them.
func (h *Handler) CommitStrandedTitles(ctx context.Context, cmd *command.CommitStrandedTitlesCmd) error {
	if h.changes == nil || h.committer == nil {
		return nil
	}
	graphDir, err := filepath.EvalSymlinks(h.graphDir)
	if err != nil {
		return fmt.Errorf("resolving graph dir: %w", err)
	}
	modified, err := h.changes.ModifiedFiles(ctx, graphDir)
	if err != nil {
		return fmt.Errorf("listing modified graph files: %w", err)
	}
	var stranded []string
	for _, path := range modified {
		rel, err := filepath.Rel(graphDir, path)
		if err != nil {
			continue
		}
		id, err := model.RelPathToID(rel)
		if err != nil {
			continue
		}
		if _, err := model.ParseID(id); err != nil {
			continue
		}
		working, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		head, err := h.changes.HeadContent(ctx, path)
		if err != nil {
			return fmt.Errorf("reading %s at HEAD: %w", path, err)
		}
		gained, err := gainedTitle(id, head, working)
		if err != nil {
			return err
		}
		if gained {
			stranded = append(stranded, path)
		}
	}
	if len(stranded) == 0 {
		return nil
	}
	if err := h.commitGraph(ctx, fmt.Sprintf("sdd: migrate commit %d stranded titles", len(stranded)), stranded...); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	if cmd.OnCommitted != nil {
		cmd.OnCommitted(len(stranded))
	}
	return nil
}

func gainedTitle(id string, head, working []byte) (bool, error) {
	before, err := model.ParseEntry(id+".md", string(head))
	if err != nil {
		return false, fmt.Errorf("parsing %s at HEAD: %w", id, err)
	}
	after, err := model.ParseEntry(id+".md", string(working))
	if err != nil {
		return false, fmt.Errorf("parsing %s: %w", id, err)
	}
	return before.Title == "" && after.Title != "", nil
}

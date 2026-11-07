package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// SetAsideJudge reads what decides whether a set-aside entry returns on its
// own: the clone's graph and the mechanical pre-flight.
type SetAsideJudge interface {
	CurrentGraph(dir string) (*model.Graph, error)
	MechanicalPreflight(g *model.Graph, entry *model.Entry) ([]query.Finding, error)
}

// settleSetAside re-adds every entry an upstream force push set aside that
// supersedes or refines an entry, names only entries the graph holds, and
// passes the mechanical pre-flight; the rest is the block it returns, nil
// once nothing is left (20261106-202055-d-tac-eyl).
func (h *Handler) settleSetAside(ctx context.Context) (*model.SyncBlocked, error) {
	if h.graphClone == nil {
		return nil, nil
	}
	setAside, err := h.graphClone.SetAside(ctx)
	if err != nil {
		return nil, err
	}
	var remaining []string
	for _, entryPath := range setAside.Entries {
		readds, err := h.readdsOnItsOwn(setAside, entryPath)
		if err != nil {
			return nil, err
		}
		if !readds {
			remaining = append(remaining, entryPath)
			continue
		}
		err = h.graphClone.ReaddSetAside(ctx, entryPath)
		if err != nil {
			return nil, fmt.Errorf("re-adding %s: %w", entryPath, err)
		}
	}
	if len(remaining) == 0 {
		return nil, nil
	}
	setAside.Entries = remaining
	block := setAside.Block()
	return &block, nil
}

func (h *Handler) readdsOnItsOwn(setAside model.SetAside, entryPath string) (bool, error) {
	if h.setAsideJudge == nil {
		return false, nil
	}
	id, err := model.EntryPathID(entryPath)
	if err != nil {
		return false, err
	}
	content, err := os.ReadFile(filepath.Join(setAside.Dir, filepath.FromSlash(entryPath)))
	if err != nil {
		return false, fmt.Errorf("reading the set-aside %s: %w", entryPath, err)
	}
	entry, err := model.ParseEntry(id+".md", string(content))
	if err != nil {
		return false, nil
	}
	graph, err := h.setAsideJudge.CurrentGraph(h.graphDir)
	if err != nil {
		return false, err
	}
	if !model.ReaddsAfterForcePush(entry, graph) {
		return false, nil
	}
	findings, err := h.setAsideJudge.MechanicalPreflight(graph, entry)
	if err != nil {
		return false, err
	}
	for _, finding := range findings {
		if finding.Severity == query.SeverityHigh {
			return false, nil
		}
	}
	return true, nil
}

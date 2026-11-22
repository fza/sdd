package local

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/model"
)

// GitTeamProofs keeps the team's proof records (application.TeamProofs) in
// the graph's repository, one file per judge key under its .sdd/proofs
// folder, each write committed on its own through the commit path of sdd's
// own graph writes, under the graph lock.
type GitTeamProofs struct {
	sddDir, graphDir string
}

// NewGitTeamProofs returns the records in sddDir, the .sdd folder of the
// repository holding the graph at graphDir.
func NewGitTeamProofs(sddDir, graphDir string) (*GitTeamProofs, error) {
	if sddDir == "" || graphDir == "" {
		return nil, fmt.Errorf("sdd: team proofs need the .sdd folder and the graph directory")
	}
	sdd, err := filepath.Abs(sddDir)
	if err != nil {
		return nil, err
	}
	graph, err := filepath.Abs(graphDir)
	if err != nil {
		return nil, err
	}
	return &GitTeamProofs{sddDir: sdd, graphDir: graph}, nil
}

// Path is the file of key's record.
func (p *GitTeamProofs) Path(key string) string {
	return filepath.Join(p.sddDir, model.TeamProofDir, key+".json")
}

func (p *GitTeamProofs) ReadTeamProof(_ context.Context, key string) ([]byte, bool, error) {
	data, err := os.ReadFile(p.Path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("reading the team proof %s: %w", key, err)
	}
	return data, true, nil
}

// WriteTeamProof writes key's record and commits it alone as
// "sdd: proof <key>".
func (p *GitTeamProofs) WriteTeamProof(ctx context.Context, key string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lock, err := lockGraph(p.graphDir)
	if err != nil {
		return err
	}
	defer unlock(lock)
	path := p.Path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	if err := (git.CLI{Dir: p.sddDir}).Commit("sdd: proof "+key, path); err != nil {
		return fmt.Errorf("committing the team proof %s: %w", key, err)
	}
	return nil
}

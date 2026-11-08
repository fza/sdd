package cliapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

func moveCmd() *cli.Command {
	return &cli.Command{
		Name:  "move",
		Usage: "Move the graph with its history to another folder or repository, or carry entries left in a former location over",
		Description: "Pass exactly one target, with the meaning sdd init gives it. Inside one repository the graph moves\n" +
			"as a rename; into or out of a separate repository its history is carried with git subtree split.\n" +
			"Run again with the same target to carry over entries left in a former location.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "graph-dir",
				Usage: "Graph folder in this repository, also the way back from a separate repository",
			},
			&cli.StringFlag{
				Name:  "project-dir",
				Usage: "Checkout on disk to move the graph out to, created and initialized when missing",
			},
			&cli.StringFlag{
				Name:  "graph-repo",
				Usage: "Address of a repository to move the graph out to, which sdd clones and pushes itself",
			},
		},
		Action: withWriteGate(func(ctx context.Context, cmd *cli.Command) error {
			target, err := moveTarget(cmd)
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			workRoot := meta.DiscoverRoot(cwd)
			if workRoot == "" {
				return errNoProject
			}
			locations, err := repos.DefaultLocations()
			if err != nil {
				return err
			}
			gate, err := commandGate()
			if err != nil {
				return err
			}
			handler := handlers.New(handlers.Options{
				Stderr:              cmd.ErrWriter,
				Gate:                gate,
				GraphClones:         repos.NewGraphClones(locations, git.GraphClones{}, gate),
				ProjectRepositories: git.Repositories{},
				GraphMoverAt:        func(dir string) handlers.GraphMover { return git.CLI{Dir: dir} },
				Strays:              finders.New(finders.Options{MoveHistory: git.MoveHistory{}}),
				SessionStoreDirs: func(sddDir string, repoID model.RepoID) (map[string]string, error) {
					return machineSessionStores(locations, sddDir, repoID)
				},
			})
			return handler.MoveGraph(ctx, &command.MoveGraphCmd{
				WorkRoot: workRoot, Target: target, LocalConfigOverride: localConfigOverride,
				BinaryVersion: cmd.Root().Version,
				OnMoved: func(record model.MoveRecord) {
					fmt.Fprintf(cmd.Writer, "Moved the graph from %s to %s: %d entries with their history (move %s).\n", record.From, record.To, record.Entries, record.ID)
				},
				OnStraysCarried: func(record model.MoveRecord, strays []model.Stray) {
					fmt.Fprintf(cmd.Writer, "Carried %d stray entries from %s to %s (move %s).\n", len(strays), record.From, record.To, record.ID)
				},
			})
		}),
	}
}

// moveTarget reads the one target flag sdd move takes.
func moveTarget(cmd *cli.Command) (model.LocationRef, error) {
	var set []string
	for _, name := range []string{"graph-dir", "project-dir", "graph-repo"} {
		if strings.TrimSpace(cmd.String(name)) != "" {
			set = append(set, "--"+name)
		}
	}
	if len(set) != 1 {
		return model.LocationRef{}, errors.New("sdd move takes exactly one of --graph-dir, --project-dir and --graph-repo")
	}
	switch set[0] {
	case "--graph-dir":
		return model.GitDirLocation(filepath.ToSlash(filepath.Clean(strings.TrimSpace(cmd.String("graph-dir"))))), nil
	case "--project-dir":
		return model.LocationRef{Kind: model.LocationGitPath, Address: strings.TrimSpace(cmd.String("project-dir"))}, nil
	}
	repo, err := model.DeriveRepoID(strings.TrimSpace(cmd.String("graph-repo")))
	if err != nil {
		return model.LocationRef{}, fmt.Errorf("--graph-repo: %w", err)
	}
	return model.LocationRef{Kind: model.LocationGitRepo, Address: string(repo)}, nil
}

// machineSessionStores names the machine-global session and staging store
// folders of the project at sddDir, the in-tree ones left out.
func machineSessionStores(locations repos.Locations, sddDir string, repoID model.RepoID) (map[string]string, error) {
	stableRoot, err := git.StableRepoRoot(filepath.Dir(sddDir))
	if err != nil {
		return nil, err
	}
	stores, err := localadapter.SessionLocations(locations.StateRoot, sddDir, repoID, stableRoot)
	if err != nil {
		return nil, err
	}
	dirs := map[string]string{}
	for _, store := range stores {
		if strings.HasPrefix(store.Sessions, sddDir) {
			continue
		}
		dirs[store.Name+" sessions"] = store.Sessions
		dirs[store.Name+" staged blobs"] = store.StagedBlobs
	}
	return dirs, nil
}

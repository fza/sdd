package cliapp

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/repos"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// relevanceCmd compacts the binary relevance ledger.
func relevanceCmd() *cli.Command {
	return &cli.Command{
		Name:  "relevance",
		Usage: "Compact the relevance verdict ledger",
		Commands: []*cli.Command{
			{
				Name:  "compact",
				Usage: "Merge the ledger's segments into one, holding every record once",
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "drop-replay", Usage: "Leave out the replayed reader labels, taking a seed back; a pair a live capture also labels stays"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					store, err := workingRelevanceStore()
					if err != nil {
						return err
					}
					if cmd.Bool("drop-replay") {
						err = store.DropReplay(ctx)
						if err != nil {
							return err
						}
						fmt.Fprintln(cmd.Writer, "Compacted the relevance ledger without its replayed labels")
						return nil
					}
					err = store.Compact(ctx)
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.Writer, "Compacted the relevance ledger")
					return nil
				},
			},
		},
	}
}

// workingRelevanceStore opens the relevance ledger of the project the
// working directory belongs to, where serve opens it.
func workingRelevanceStore() (*localadapter.FilesystemRelevanceStore, error) {
	sddDir, err := resolveSDDDir()
	if err != nil {
		return nil, err
	}
	dir, err := workingRelevanceDir(sddDir)
	if err != nil {
		return nil, err
	}
	return localadapter.NewFilesystemRelevanceStore(dir)
}

// workingRelevanceDir is the ledger folder of the project at sddDir.
func workingRelevanceDir(sddDir string) (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	locations, err := repos.DefaultLocations()
	if err != nil {
		return "", err
	}
	stableRepoRoot, err := git.StableRepoRoot(filepath.Dir(sddDir))
	if err != nil {
		return "", err
	}
	key, err := persistentIndexRepoKey(cfg, stableRepoRoot)
	if err != nil {
		return "", err
	}
	return relevanceLedgerDir(locations.StateRoot, key), nil
}

package cliapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/repos"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// relevanceCmd converts the binary relevance ledger to and from JSON and
// compacts it (20261109-004506-d-cpt-udz, D16 and D17).
func relevanceCmd() *cli.Command {
	return &cli.Command{
		Name:  "relevance",
		Usage: "Inspect, restore and compact the relevance verdict ledger",
		Commands: []*cli.Command{
			{
				Name:  "export",
				Usage: "Write the ledger as JSON Lines: reader verdicts, scores, then drift steps",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					store, err := workingRelevanceStore()
					if err != nil {
						return err
					}
					return sdd.ExportRelevanceJSON(ctx, store, cmd.Writer)
				},
			},
			{
				Name:      "import",
				Usage:     "Append the records of a JSON Lines file, as export writes it, as one segment",
				ArgsUsage: "<file|->",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return fmt.Errorf("pass exactly one file, or - for standard input")
					}
					store, err := workingRelevanceStore()
					if err != nil {
						return err
					}
					input := cmd.Reader
					if name := cmd.Args().First(); name != "-" {
						file, err := os.Open(name)
						if err != nil {
							return err
						}
						defer file.Close()
						input = file
					}
					count, err := sdd.ImportRelevanceJSON(ctx, store, input)
					if err != nil {
						return err
					}
					fmt.Fprintf(cmd.Writer, "Imported %d records\n", count)
					return nil
				},
			},
			{
				Name:  "compact",
				Usage: "Merge the ledger's segments into one, holding every record once",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					store, err := workingRelevanceStore()
					if err != nil {
						return err
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
func workingRelevanceStore() (relevanceStore, error) {
	sddDir, err := resolveSDDDir()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	locations, err := repos.DefaultLocations()
	if err != nil {
		return nil, err
	}
	stableRepoRoot, err := git.StableRepoRoot(filepath.Dir(sddDir))
	if err != nil {
		return nil, err
	}
	return projectRelevanceStore(sddDir, locations.StateRoot, persistentIndexRepoKey(cfg, stableRepoRoot), cfg)
}

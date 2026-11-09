package cliapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// eventsCmd converts the binary event log to and from JSON, so its segments
// stay inspectable and repairable (20261109-004506-d-cpt-udz, D16).
func eventsCmd() *cli.Command {
	return &cli.Command{
		Name:  "events",
		Usage: "Inspect and restore the project event store",
		Commands: []*cli.Command{
			{
				Name:  "export",
				Usage: "Write stored events as JSON Lines, one event per line ordered by time",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "channel", Usage: "Export only this channel (repeatable); default every channel"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					store, err := workingProjectEventStore()
					if err != nil {
						return err
					}
					return sdd.ExportEventsJSON(ctx, store, sdd.EventQuery{Channels: cmd.StringSlice("channel")}, cmd.Writer)
				},
			},
			{
				Name:  "compact",
				Usage: "Merge each channel's segments into one, holding every event once",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					store, err := workingProjectEventStore()
					if err != nil {
						return err
					}
					err = store.Compact(ctx)
					if err != nil {
						return err
					}
					fmt.Fprintln(cmd.Writer, "Compacted the event store")
					return nil
				},
			},
			{
				Name:      "import",
				Usage:     "Append the events of a JSON Lines file, as export writes it, that the store does not hold yet",
				ArgsUsage: "<file|->",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return fmt.Errorf("pass exactly one file, or - for standard input")
					}
					store, err := workingProjectEventStore()
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
					added, err := sdd.ImportEventsJSON(ctx, store, input)
					if err != nil {
						return err
					}
					fmt.Fprintf(cmd.Writer, "Imported %d events\n", added)
					return nil
				},
			},
		},
	}
}

// workingProjectEventStore opens the event store of the project the working
// directory belongs to.
func workingProjectEventStore() (*localadapter.FilesystemEventStore, error) {
	sddDir, err := resolveSDDDir()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return projectEventStore(sddDir, meta.ResolveGraphDir(filepath.Dir(sddDir), cfg), cfg)
}

// projectEventStore opens the event store under sddDir with the sharing the
// config sets. Compaction drops the recheck items an entry in graphDir
// healed (20261109-004506-d-cpt-udz, D17).
func projectEventStore(sddDir, graphDir string, cfg *model.PerRepoConfig) (*localadapter.FilesystemEventStore, error) {
	var err error
	var gitChannels []string
	if cfg != nil {
		gitChannels, err = cfg.Events.GitChannels()
		if err != nil {
			return nil, err
		}
	}
	return localadapter.NewFilesystemEventStore(localadapter.FilesystemEventStoreOptions{
		Dir: filepath.Join(sddDir, model.EventsDirName), GitChannels: gitChannels, Settled: healedRecheck(graphDir, cfg),
	})
}

// healedRecheck names the recheck events whose item a graph entry healed.
func healedRecheck(graphDir string, cfg *model.PerRepoConfig) localadapter.EventSettler {
	return func(_ context.Context, channel string, events []sdd.Event) ([]string, error) {
		if channel != model.RecheckChannel {
			return nil, nil
		}
		var items []model.RecheckItem
		for _, event := range events {
			if event.Type != model.RecheckEventType {
				continue
			}
			item, err := model.RecheckItemFromEvent(event.ID, event.Time, event.Subject.Entries, event.Payload)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		if len(items) == 0 {
			return nil, nil
		}
		g, err := finders.New(finders.Options{Config: cfg}).LoadGraph(graphDir)
		if err != nil {
			return nil, err
		}
		return model.HealedRecheck(g, items), nil
	}
}

package cliapp

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cliout"
	clitui "github.com/networkteam/sdd/internal/cliout/tui"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// migrateCmd upgrades a graph to the schema version this binary writes. It
// states what the migration changes and what the backfill costs, and runs only
// on the user's explicit approval: an answer on stdin or --yes.
func migrateCmd() *cli.Command {
	return &cli.Command{
		Name:  "migrate",
		Usage: "Upgrade the graph to this binary's schema version and fill missing titles, after approval",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "yes",
				Usage: "Approve the migration without asking",
			},
			&cli.BoolFlag{
				Name:  "stamp-only",
				Usage: "Stamp the schema version without the backfill; a later `sdd migrate` fills what is missing",
			},
			&cli.StringFlag{
				Name:  "provider",
				Usage: "LLM provider (claude-cli, anthropic, openai, ollama) — overrides config",
			},
			&cli.StringFlag{
				Name:  "model",
				Usage: "LLM model identifier — overrides config",
			},
			&cli.IntFlag{
				Name:  "concurrency",
				Usage: "Worker pool size for the backfill — overrides config",
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "Timeout per generation (e.g. 60s, 1m)",
				Value: 60 * time.Second,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			sddDir, err := resolveSDDDir()
			if err != nil {
				return err
			}
			finder, err := newReadFinder()
			if err != nil {
				return err
			}
			status, err := finder.SchemaStatus(ctx, query.SchemaStatusQuery{SDDDir: sddDir, BinaryVersion: cmd.Root().Version, BinarySchemaVersion: model.CurrentGraphSchemaVersion})
			if err != nil {
				return fmt.Errorf("schema check: %w", err)
			}
			if !status.MetaExists {
				return fmt.Errorf("no %s in %s; run `sdd init` first", model.SchemaMetaFileName, sddDir)
			}
			from := status.Meta.GraphSchemaVersion
			if from > model.CurrentGraphSchemaVersion {
				return fmt.Errorf("graph schema version %d is newer than this binary's schema version %d; upgrade sdd to continue", from, model.CurrentGraphSchemaVersion)
			}
			out := cmd.ErrWriter
			handler, err := summarizeHandler(cmd)
			if err != nil {
				return err
			}
			err = handler.CommitStrandedTitles(ctx, &command.CommitStrandedTitlesCmd{
				OnCommitted: func(count int) {
					fmt.Fprintf(out, "committed %d entries an earlier backfill titled but did not commit\n", count)
				},
			})
			if err != nil {
				return err
			}
			graph, err := loadGraph(cmd)
			if err != nil {
				return err
			}
			withSummary, titleOnly := model.TitleBackfill(graph.Entries)
			stampOnly := cmd.Bool("stamp-only")
			if from == model.CurrentGraphSchemaVersion && (stampOnly || withSummary+titleOnly == 0) {
				fmt.Fprintf(cmd.ErrWriter, "graph is at schema version %d; nothing to migrate\n", from)
				return nil
			}

			if from < model.CurrentGraphSchemaVersion {
				fmt.Fprintf(out, "Graph schema %d -> %d:\n", from, model.CurrentGraphSchemaVersion)
				for _, change := range model.SchemaChanges(from, model.CurrentGraphSchemaVersion) {
					fmt.Fprintf(out, "  - %s\n", change)
				}
			}
			if stampOnly {
				fmt.Fprintf(out, "Backfill skipped: %d entries owe a title or summary; a later `sdd migrate` fills them.\n", titleOnly+withSummary)
			} else {
				fmt.Fprintf(out, "Backfill: %d entries get a title beside their stored summary, %d entries get a summary and a title -> %d model calls.\n", titleOnly, withSummary, titleOnly+withSummary)
				if from < model.CurrentGraphSchemaVersion {
					fmt.Fprintln(out, "The schema version is stamped first; an interrupted backfill continues on the next `sdd migrate`.")
				} else {
					fmt.Fprintln(out, "An interrupted backfill continues on the next `sdd migrate`.")
				}
			}
			if !cmd.Bool("yes") {
				fmt.Fprint(out, "Proceed? [y/N] ")
				answer, _ := bufio.NewReader(cmd.Reader).ReadString('\n')
				answer = strings.ToLower(strings.TrimSpace(answer))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(out, "migration not approved; graph unchanged")
					return nil
				}
			}

			err = handler.MigrateSchema(ctx, &command.MigrateSchemaCmd{
				SDDDir:        sddDir,
				SchemaVersion: model.CurrentGraphSchemaVersion,
				OnStamped: func(from, to int) {
					fmt.Fprintf(out, "stamped graph schema %d -> %d\n", from, to)
				},
			})
			if err != nil {
				return err
			}
			if stampOnly {
				return nil
			}
			backfill := &command.SummarizeCmd{
				Concurrency: int(cmd.Int("concurrency")),
				Model:       cmd.String("model"),
				OnSummarized: func(id, summary string) {
					fmt.Fprintf(out, "  summarized %s\n", id)
				},
				OnTitled: func(id, title string) {
					fmt.Fprintf(out, "  titled %s\n", id)
				},
			}
			if !cliout.IsInteractive(out) {
				return handler.Summarize(ctx, backfill)
			}
			progress := newBackfillProgress(backfill)
			_, err = clitui.Interactive(ctx, transientViewPolicy(),
				clitui.View{Reader: cmd.Reader, Writer: out, InitialPhase: model.PhaseSummarizing, Progress: progress},
				func(ctx context.Context) (struct{}, error) {
					return struct{}{}, handler.Summarize(ctx, backfill)
				})
			return err
		},
	}
}

// newBackfillProgress reroutes a backfill's per-entry callbacks onto a footer
// reporter: on a terminal the bar replaces the line per entry.
func newBackfillProgress(backfill *command.SummarizeCmd) *cliout.Reporter {
	reporter := cliout.NewReporter()
	reporter.SetUnit("entries")
	backfill.OnPlanned = func(n int) {
		reporter.SetPhase(model.PhaseSummarizing)
		reporter.SetTotal(n)
	}
	advance := func(id, _ string) {
		reporter.Add(1)
		reporter.SetNote(id)
	}
	backfill.OnSummarized = advance
	backfill.OnTitled = advance
	return reporter
}

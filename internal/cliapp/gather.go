package cliapp

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// gatherCmd prints the grounding serve of a draft (20261109-004506-d-cpt-udz,
// 5.3). The CLI credits no capture and never cuts or points (R14).
func gatherCmd() *cli.Command {
	return &cli.Command{
		Name:      "gather",
		Usage:     "Print the grounding reading of a draft: every live brief on a small graph, else its searches and topic listings",
		ArgsUsage: "[entry-id...]",
		Flags: append(embeddingFlags(),
			&cli.StringFlag{Name: "draft", Usage: "Draft text the searches are written from"},
			&cli.StringFlag{Name: "draft-file", Usage: "File holding the draft text"},
			&cli.StringFlag{Name: "kind", Usage: "The draft's entry kind"},
			&cli.StringSliceFlag{Name: "topic", Usage: "A draft topic label, listed in full (repeatable)"},
			&cli.StringFlag{Name: "query", Aliases: []string{"q"}, Usage: "Semantic search phrase, instead of the query writer"},
			&cli.StringSliceFlag{Name: "term", Aliases: []string{"t"}, Usage: "Keyword term, one search each, instead of the query writer (repeatable)"},
			&cli.StringFlag{Name: "subject", Usage: "An existing entry the reading grounds: its text stands in for the draft, and stored verdicts against it answer repeat pairs"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			draft := cmd.String("draft")
			if path := cmd.String("draft-file"); path != "" {
				if draft != "" {
					return fmt.Errorf("pass --draft or --draft-file, not both")
				}
				content, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("reading %s: %w", path, err)
				}
				draft = string(content)
			}
			dir, err := resolveGraphDir(cmd)
			if err != nil {
				return err
			}
			sddDir, err := resolveSDDDir()
			if err != nil {
				return err
			}
			runner, err := newRunner(cmd, "")
			if err != nil {
				return err
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			reg, _, err := defaultRepos()
			if err != nil {
				return err
			}
			application, runtime, identity, err := buildLocalApplication(ctx, cmd, dir, sddDir, cfg, reg, runner)
			if err != nil {
				return err
			}
			result, err := application.Gather(ctx, identity, runtime.Project().ID, sdd.GatherRequest{
				SyncMode: sdd.SearchSyncAll, Draft: draft, Kind: cmd.String("kind"), Topics: cmd.StringSlice("topic"),
				IDs: cmd.Args().Slice(), Query: cmd.String("query"), Terms: cmd.StringSlice("term"), Subject: cmd.String("subject"),
			})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.Writer, result.Text)
			return err
		},
	}
}

// refuseUngroundedNew refuses a direct `sdd new` under grounding.enforce: the
// CLI holds no capture whose reading the gate could judge, so the write goes
// through the engine capture, or records its bypass (D3, D4). A dry run
// writes nothing and actor and role drafts are exempt (D38).
func refuseUngroundedNew(cfg *model.PerRepoConfig, ncmd *command.NewEntryCmd) error {
	kind := ncmd.Kind
	if kind == "" {
		kind = model.DefaultKindForType(ncmd.Type)
	}
	if !groundingPolicy(cfg).Enforce || ncmd.SkipGrounding || ncmd.DryRun || model.GroundingExemptKind(kind) {
		return nil
	}
	return fmt.Errorf("grounding.enforce is on, so sdd new does not write a %s directly: capture it through the engine (start_procedure capture over the MCP server), whose gather reading the gate judges, or pass --skip-grounding to record a bypass", kind)
}

// groundingPolicy is the resolved grounding block of cfg.
func groundingPolicy(cfg *model.PerRepoConfig) sdd.GroundingPolicy {
	var block model.GroundingConfig
	if cfg != nil {
		block = cfg.Grounding
	}
	policy := block.Policy()
	return sdd.GroundingPolicy{
		Enforce: policy.Enforce, DepthDown: policy.DepthDown, DepthUp: policy.DepthUp,
		BriefAllBelow: policy.BriefAllBelow, PrefilterFrom: policy.PrefilterFrom, Prefilter: policy.Prefilter,
	}
}

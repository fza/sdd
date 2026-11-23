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

// gatherCmd prints the grounding serve of a draft. It never cuts or points,
// and its reading credits no capture: only an engine capture's own gather
// grounds it.
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
			&cli.StringSliceFlag{Name: "term", Aliases: []string{"t"}, Usage: "Keyword term, matched as whole words; one search each, instead of the query writer (repeatable)"},
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
			application, project, identity, err := workingApplication(ctx, cmd)
			if err != nil {
				return err
			}
			result, err := application.Gather(ctx, identity, project, sdd.GatherRequest{
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

// refuseUngroundedNew refuses a direct `sdd new` under grounding enforcement: the
// CLI holds no capture whose reading the gate could judge, so the write goes
// through the engine capture, or records its bypass. A dry run writes nothing
// and actor and role drafts are exempt.
func refuseUngroundedNew(cfg *model.PerRepoConfig, ncmd *command.NewEntryCmd) error {
	kind := ncmd.Kind
	if kind == "" {
		kind = model.DefaultKindForType(ncmd.Type)
	}
	var block model.VerifyConfig
	if cfg != nil {
		block = cfg.Verify
	}
	mode := block.ModeFor(kind)
	enforce, _ := mode.Settings()
	if !enforce || ncmd.SkipGrounding || ncmd.DryRun || model.GroundingExemptKind(kind) {
		return nil
	}
	return fmt.Errorf("verify.mode %s enforces grounding, so sdd new does not write a %s directly: capture it through the engine (start_procedure capture over the MCP server), whose gather reading the gate judges, or pass --skip-grounding to write it with grounding: skipped and confidence low", mode, kind)
}

// groundingPolicy is the resolved grounding part of cfg's verify block.
func groundingPolicy(cfg *model.PerRepoConfig) sdd.GroundingPolicy {
	var block model.VerifyConfig
	var recheck model.RecheckConfig
	if cfg != nil {
		block = cfg.Verify
		recheck = cfg.Recheck
	}
	policy := block.GroundingPolicy()
	return sdd.GroundingPolicy{
		Enforce: policy.Enforce, DepthDown: policy.DepthDown, DepthUp: policy.DepthUp,
		BriefAllBelow: policy.BriefAllBelow, PrefilterFrom: policy.PrefilterFrom, Prefilter: policy.Prefilter,
		RecheckResponsible: recheck.Responsible,
	}
}

func verifierPolicy(cfg *model.PerRepoConfig) sdd.VerifierPolicy {
	var block model.VerifyConfig
	if cfg != nil {
		block = cfg.Verify
	}
	policy := block.VerifierPolicy()
	kinds, stricter := map[string]string{}, map[string][]string{}
	for _, kind := range append(model.SignalKindValues(), model.DecisionKindValues()...) {
		kinds[string(kind)] = string(block.ModeFor(kind))
		for _, other := range block.StricterKinds(kind) {
			stricter[string(kind)] = append(stricter[string(kind)], string(other))
		}
	}
	return sdd.VerifierPolicy{
		Mode: string(policy.Mode), Skip: string(policy.Skip), Runner: string(policy.Runner),
		Claude: sdd.VerifierAgent(policy.Claude), RoundTimeout: policy.RoundTimeout, IdleTimeout: policy.IdleTimeout,
		Kinds: kinds, StricterKinds: stricter,
	}
}

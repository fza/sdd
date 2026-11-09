package cliapp

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/llm/factory"
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
			runner, err := newRunner(cmd, "")
			if err != nil {
				return err
			}
			served, err := buildServedProject(ctx, cmd, runner)
			if err != nil {
				return err
			}
			application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: served.access, Sessions: served.sessions, StagedBlobs: served.blobs})
			if err != nil {
				return err
			}
			identity := sdd.RequestIdentity{Subject: "local"}
			runtime := served.access.runtime
			result, err := application.Gather(ctx, identity, runtime.Project().ID, sdd.GatherRequest{
				SyncMode: sdd.SearchSyncAll, Draft: draft, Kind: cmd.String("kind"), Topics: cmd.StringSlice("topic"),
				IDs: cmd.Args().Slice(), Query: cmd.String("query"), Terms: cmd.StringSlice("term"),
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
		BriefAllBelow: policy.BriefAllBelow, PrefilterFrom: policy.PrefilterFrom,
	}
}

func verifierPolicy(cfg *model.PerRepoConfig) sdd.VerifierPolicy {
	if cfg == nil {
		return sdd.VerifierPolicy{}
	}
	policy := cfg.Verifier.Policy()
	return sdd.VerifierPolicy{Mode: string(policy.Mode), Skip: string(policy.Skip), Launch: string(policy.Launch)}
}

// modelWindows detects each model's context window once per process.
var modelWindows = &factory.WindowDetector{}

// modelWindow sizes a light verifier call for the runner cfg configures: the
// window llm.context_tokens sets or the provider reports, and the answer
// reserve llm.max_tokens sets, else the model's reported output limit,
// capped (20261109-004729-d-cpt-xs5, V-D12).
func modelWindow(cfg model.LLMConfig) func(context.Context) (sdd.ModelWindow, error) {
	return func(ctx context.Context) (sdd.ModelWindow, error) {
		window, err := modelWindows.Detect(ctx, cfg)
		if err != nil {
			return sdd.ModelWindow{}, err
		}
		reserve, err := cfg.ResolvedMaxTokens()
		if err != nil {
			return sdd.ModelWindow{}, err
		}
		if reserve == 0 {
			reserve = min(window.MaxOutput, model.LightUncappedOutput)
			if reserve <= 0 {
				reserve = model.LightUncappedOutput
			}
		}
		return sdd.ModelWindow{Tokens: window.Tokens, OutputReserve: reserve}, nil
	}
}

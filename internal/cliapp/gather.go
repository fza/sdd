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

// gatherCmd prints the grounding serve of a draft. It never cuts or points;
// only with --session does the reading credit a capture.
func gatherCmd() *cli.Command {
	return &cli.Command{
		Name:      "gather",
		Usage:     "Print the grounding reading of a draft: a search with its own text, your searches and its topic listings",
		ArgsUsage: "[entry-id...]",
		Flags: append(append(embeddingFlags(),
			&cli.StringFlag{Name: "draft", Usage: "Draft text, searched by meaning as it stands"},
			&cli.StringFlag{Name: "draft-file", Usage: "File holding the draft text"},
			&cli.StringFlag{Name: "kind", Usage: "The draft's entry kind"},
			&cli.StringSliceFlag{Name: "topic", Usage: "A draft topic label, listed in full (repeatable)"},
			&cli.StringFlag{Name: "query", Aliases: []string{"q"}, Usage: "Search phrase, searched by meaning beside the draft's text"},
			&cli.StringSliceFlag{Name: "term", Aliases: []string{"t"}, Usage: "Keyword term, matched as whole words; one search each (repeatable)"},
			&cli.StringFlag{Name: "subject", Usage: "An existing entry the reading grounds: its text stands in for the draft, and stored verdicts against it answer repeat pairs"},
			&cli.BoolFlag{Name: "wait", Usage: "Have the ranking judge score until the minimum is judged, without its time limit"},
			&cli.BoolFlag{Name: "rejudge", Usage: "Have the ranking judge score every candidate against the draft as it stands, the capture's kept judgements included"},
		), sessionFlags()...),
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
			session, err := joinSessionOn(ctx, cmd, application, identity)
			if err != nil {
				return err
			}
			var result sdd.GatherResult
			if session != nil {
				result, err = sessionGather(ctx, cmd, session, draft)
			} else {
				result, err = application.Gather(ctx, identity, project, sdd.GatherRequest{
					SyncMode: sdd.SearchSyncAll, Draft: draft, Kind: cmd.String("kind"), Topics: cmd.StringSlice("topic"),
					IDs: cmd.Args().Slice(), Query: cmd.String("query"), Terms: cmd.StringSlice("term"), Subject: cmd.String("subject"), Wait: cmd.Bool("wait"),
				})
			}
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.Writer, result.Text)
			return err
		},
	}
}

// sessionGatherUnsupported are the gather flags a session reading takes from
// its capture's draft instead.
var sessionGatherUnsupported = []string{"kind", "subject"}

// sessionGather runs the reading inside the session: naming its capture, the
// reading credits that capture as the MCP gather does; without one it only
// logs.
func sessionGather(ctx context.Context, cmd *cli.Command, session *cliSession, draft string) (sdd.GatherResult, error) {
	for _, name := range sessionGatherUnsupported {
		if cmd.IsSet(name) {
			return sdd.GatherResult{}, fmt.Errorf("--%s does not apply with --session: the capture's draft sets it", name)
		}
	}
	instance, err := session.capture(cmd, false)
	if err != nil {
		return sdd.GatherResult{}, err
	}
	return session.workflow.Gather(ctx, session.identity, sdd.WorkflowGatherRequest{
		Instance: instance, IDs: cmd.Args().Slice(), Draft: draft, Topics: cmd.StringSlice("topic"),
		Query: cmd.String("query"), Terms: cmd.StringSlice("term"), Wait: cmd.Bool("wait"), Rejudge: cmd.Bool("rejudge"), SyncMode: sdd.SearchSyncAll,
	})
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
	return fmt.Errorf("verify.mode %s enforces grounding, so sdd new does not write a %s directly: capture it through the engine (start_procedure capture over the MCP server), whose gather reading the gate judges, write it into that capture with sdd new --session, or pass --skip-grounding to record a bypass", mode, kind)
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
		PrefilterFrom: policy.PrefilterFrom, Prefilter: policy.Prefilter, JudgeTimeout: policy.JudgeTimeout, JudgeMinimum: policy.JudgeMinimum,
		AutoCalibrate: policy.AutoCalibrate, RecheckResponsible: recheck.Responsible,
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

// modelWindows detects each model's context window once per process.
var modelWindows = &factory.WindowDetector{}

// modelWindow sizes a prose verifier call for the runner cfg configures: the
// window llm.context_tokens sets or the provider reports, and the answer
// reserve llm.max_tokens sets, else the model's reported output limit,
// capped.
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

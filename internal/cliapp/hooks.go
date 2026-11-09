package cliapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cliout"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
)

// hooksCmd lists the declared hooks with their confirmation state and
// confirms pending ones (20261109-213250-d-tac-bve).
func hooksCmd() *cli.Command {
	return &cli.Command{
		Name:  "hooks",
		Usage: "List the hooks declared for this project and this machine with their confirmation state",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			result, err := declaredHooks()
			if err != nil {
				return err
			}
			return presenters.RenderHooks(cmd.Writer, result.Hooks)
		},
		Commands: []*cli.Command{
			{
				Name:  "trust",
				Usage: "Review each pending hook and confirm it to run on this machine",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					result, err := declaredHooks()
					if err != nil {
						return err
					}
					if len(pendingHooks(result.Hooks)) == 0 {
						fmt.Fprintln(cmd.Writer, "no hooks wait for confirmation")
						return nil
					}
					err = trustHooks(ctx, cmd, pendingHooks(result.Hooks))
					return err
				},
			},
		},
	}
}

// hookSources names where the project governing the working directory and
// this machine declare hooks; outside a project only the user-global config.
func hookSources() (hooks.Sources, error) {
	locations, err := repos.DefaultLocations()
	if err != nil {
		return hooks.Sources{}, err
	}
	sources := hooks.Sources{GlobalConfig: locations.ConfigPath, LocalConfig: localConfigOverride}
	project, err := resolveProject()
	if errors.Is(err, errNoProject) {
		return sources, nil
	}
	if err != nil {
		return hooks.Sources{}, err
	}
	sources.Project = project
	return sources, nil
}

func declaredHooks() (*query.HooksResult, error) {
	sources, err := hookSources()
	if err != nil {
		return nil, err
	}
	locations, err := repos.DefaultLocations()
	if err != nil {
		return nil, err
	}
	finder, err := newReadFinder()
	if err != nil {
		return nil, err
	}
	return finder.Hooks(query.HooksQuery{
		GlobalConfig: sources.GlobalConfig, Root: sources.Project.Root, WorkRoot: sources.Project.WorkRoot,
		GraphRepo: sources.Project.GraphRepo, LocalConfig: sources.LocalConfig, StateRoot: locations.StateRoot,
	})
}

func pendingHooks(all []model.Hook) []model.Hook {
	var pending []model.Hook
	for _, hook := range all {
		if hook.State != model.HookConfirmed {
			pending = append(pending, hook)
		}
	}
	return pending
}

// errTrustNeedsTerminal refuses confirming hooks without a person at a
// terminal to answer for each one.
var errTrustNeedsTerminal = errors.New("sdd hooks trust confirms only on a terminal: a person reviews and confirms each hook; run it in a terminal")

// trustHooks shows each pending hook's review on the terminal and confirms
// the ones the person accepts. Without a terminal it confirms nothing.
func trustHooks(ctx context.Context, cmd *cli.Command, pending []model.Hook) error {
	if !cliout.IsTerminalReader(cmd.Reader) {
		return errTrustNeedsTerminal
	}
	locations, err := repos.DefaultLocations()
	if err != nil {
		return err
	}
	finder, err := newReadFinder()
	if err != nil {
		return err
	}
	handler := handlers.New(handlers.Options{})
	for _, hook := range pending {
		review, err := finder.HookReview(query.HookReviewQuery{Hook: hook})
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.Writer, presenters.HookReviewText(review))
		accepted, err := promptConfirmation(cmd, fmt.Sprintf("Run this %s hook on this machine?", hook.Event))
		if err != nil {
			return err
		}
		if !accepted {
			fmt.Fprintf(cmd.Writer, "  left %s\n\n", hook.State)
			continue
		}
		if err := handler.ConfirmHook(ctx, &command.ConfirmHookCmd{StateRoot: locations.StateRoot, Hook: hook}); err != nil {
			return err
		}
		fmt.Fprint(cmd.Writer, "  confirmed\n\n")
	}
	return nil
}

// offerPendingHooks lets `sdd init` confirm the project's pending hooks on a
// terminal, and elsewhere names how many wait for `sdd hooks trust`.
func offerPendingHooks(ctx context.Context, cmd *cli.Command) error {
	result, err := declaredHooks()
	if err != nil {
		return err
	}
	pending := pendingHooks(result.Hooks)
	if len(pending) == 0 {
		return nil
	}
	if !cliout.IsTerminalReader(cmd.Reader) {
		fmt.Fprintf(cmd.ErrWriter, "%d hook(s) of this project wait for confirmation; review them with `sdd hooks trust` in a terminal\n", len(pending))
		return nil
	}
	return trustHooks(ctx, cmd, pending)
}

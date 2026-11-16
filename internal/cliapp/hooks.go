package cliapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/cliout"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
)

// hooksCmd lists the declared hooks with whether they may run on this
// machine and allows the hooks of the project's repositories.
func hooksCmd() *cli.Command {
	return &cli.Command{
		Name:  "hooks",
		Usage: "List the hooks declared for this project and this machine with whether they may run here",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			result, err := declaredHooks(ctx)
			if err != nil {
				return err
			}
			return presenters.RenderHooks(cmd.Writer, result.Hooks)
		},
		Commands: []*cli.Command{
			{
				Name:  "allow",
				Usage: "Review the hooks of each of the project's repositories not allowed on this machine as they are now, and allow those you accept",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					sources, stateRoot, err := hookSources()
					if err != nil {
						return err
					}
					if !cliout.IsTerminalReader(cmd.Reader) {
						return errAllowNeedsTerminal
					}
					reviews, err := pendingReviews(ctx, sources, stateRoot)
					if err != nil {
						return err
					}
					if len(reviews) == 0 {
						fmt.Fprintln(cmd.Writer, "no hooks wait to be allowed")
						return nil
					}
					return allowHooks(ctx, cmd, sources, stateRoot, reviews)
				},
			},
		},
	}
}

// hookSources names where the project governing the working directory and
// this machine declare hooks, outside a project only the user-global config,
// and the state root holding the allow record.
func hookSources() (hooks.Sources, string, error) {
	locations, err := repos.DefaultLocations()
	if err != nil {
		return hooks.Sources{}, "", err
	}
	sources := hooks.Sources{GlobalConfig: locations.ConfigPath, LocalConfig: localConfigOverride, Tracker: git.TrackedFiles{}}
	project, err := resolveProject()
	if errors.Is(err, errNoProject) {
		return sources, locations.StateRoot, nil
	}
	if err != nil {
		return hooks.Sources{}, "", err
	}
	sources.Project = project
	return sources, locations.StateRoot, nil
}

func declaredHooks(ctx context.Context) (*query.HooksResult, error) {
	sources, stateRoot, err := hookSources()
	if err != nil {
		return nil, err
	}
	finder, err := newReadFinder()
	if err != nil {
		return nil, err
	}
	return finder.Hooks(ctx, query.HooksQuery{Sources: sources, StateRoot: stateRoot})
}

func pendingReviews(ctx context.Context, sources hooks.Sources, stateRoot string) ([]model.HookReview, error) {
	finder, err := newReadFinder()
	if err != nil {
		return nil, err
	}
	return finder.HookReviews(ctx, query.HookReviewsQuery{Sources: sources, StateRoot: stateRoot})
}

// errAllowNeedsTerminal refuses allowing hooks without a person at a
// terminal to answer for each repository.
var errAllowNeedsTerminal = errors.New("sdd hooks allow allows only on a terminal: a person reviews and allows each repository's hooks; run it in a terminal")

// allowHooks shows each review on the terminal and allows the repositories
// the person accepts.
func allowHooks(ctx context.Context, cmd *cli.Command, sources hooks.Sources, stateRoot string, reviews []model.HookReview) error {
	var accepted []model.HookReview
	for _, review := range reviews {
		fmt.Fprintln(cmd.Writer, presenters.HookReviewText(review))
		yes, err := promptConfirmation(cmd, "Allow these hooks to run on this machine?")
		if err != nil {
			return err
		}
		if !yes {
			fmt.Fprintf(cmd.Writer, "  left %s\n\n", review.State)
			continue
		}
		accepted = append(accepted, review)
	}
	if len(accepted) == 0 {
		return nil
	}
	handler := handlers.New(handlers.Options{})
	if err := handler.AllowHooks(ctx, &command.AllowHooksCmd{Sources: sources, StateRoot: stateRoot, Reviews: accepted}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.Writer, "  allowed; a change to them, or to a file git tracks in the hooks folder, needs `sdd hooks allow` again\n\n")
	return nil
}

// offerPendingHooks lets `sdd init` allow the project's hooks on a terminal,
// and elsewhere names how many repositories wait for `sdd hooks allow`.
func offerPendingHooks(ctx context.Context, cmd *cli.Command) error {
	sources, stateRoot, err := hookSources()
	if err != nil {
		return err
	}
	result, err := declaredHooks(ctx)
	if err != nil {
		return err
	}
	pending := 0
	for _, hook := range result.Hooks {
		if hook.State != model.HookAllowed {
			pending++
		}
	}
	if pending == 0 {
		return nil
	}
	if !cliout.IsTerminalReader(cmd.Reader) {
		fmt.Fprintf(cmd.ErrWriter, "%d hook(s) of this project wait to be allowed; review them with `sdd hooks allow` in a terminal\n", pending)
		return nil
	}
	reviews, err := pendingReviews(ctx, sources, stateRoot)
	if err != nil {
		return err
	}
	return allowHooks(ctx, cmd, sources, stateRoot, reviews)
}

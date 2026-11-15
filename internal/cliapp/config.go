package cliapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/urfave/cli/v3"
)

// configCmd is the CLI surface over the config overlay: the bare command
// prints the effective merged config with per-value provenance, `get` reads
// effective values (one key, a subtree, or everything), and `set`/`unset`
// write the global or local layer with comment-preserving patches.
func configCmd() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Show and edit SDD configuration (global-first overlay with per-repo override)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "format",
				Usage: "Output format: text (default) or json",
				Value: "text",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runEffectiveConfig(cmd, "")
		},
		Commands: []*cli.Command{
			{
				Name:      "get",
				Usage:     "Print effective config values — one key, a subtree (e.g. llm), or everything",
				ArgsUsage: "[<key-or-prefix>]",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() > 1 {
						return fmt.Errorf("usage: sdd config get [<key-or-prefix>]")
					}
					return runConfigGet(cmd.Writer, cmd.Args().First())
				},
			},
			{
				Name:      "set",
				Usage:     "Set a config key (user-global by default; --local for the machine-local layer)",
				ArgsUsage: "<key> <value>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "local",
						Usage: "Write to the machine-local layer (.sdd/config.local.yaml, or --local-config) instead of the user-global config",
					},
					&cli.StringFlag{
						Name:  "repo",
						Usage: "Set the key's override for one repository id, e.g. --repo github.com/org/graph connected.write true",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 2 {
						return fmt.Errorf("usage: sdd config set [--local] [--repo <repo-id>] <key> <value>")
					}
					target := "global"
					if cmd.Bool("local") {
						target = "local"
					}
					set := &command.ConfigSetCmd{Target: target, Key: cmd.Args().Get(0), Value: cmd.Args().Get(1), Repo: cmd.String("repo")}
					return runConfigSet(ctx, cmd.Writer, cmd.ErrWriter, set)
				},
			},
			{
				Name:      "unset",
				Usage:     "Remove a config key (user-global by default; --local for the machine-local layer)",
				ArgsUsage: "<key>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "local",
						Usage: "Remove from the machine-local layer (.sdd/config.local.yaml, or --local-config) instead of the user-global config",
					},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 1 {
						return fmt.Errorf("usage: sdd config unset [--local] <key>")
					}
					target := "global"
					if cmd.Bool("local") {
						target = "local"
					}
					return runConfigUnset(ctx, cmd.Writer, cmd.ErrWriter, target, cmd.Args().First())
				},
			},
		},
	}
}

// writesConfig reports whether the invocation is `sdd config set` or `unset`.
func writesConfig(root *cli.Command) bool {
	lineage := invokedCommand(root).Lineage()
	return len(lineage) == 3 && lineage[1].Name == "config" && slices.Contains([]string{"set", "unset"}, lineage[0].Name)
}

// invokedCommand follows the parsed arguments from root to the command that
// runs, so flags between command names do not shift which one it is.
func invokedCommand(root *cli.Command) *cli.Command {
	cmd := root
	for {
		sub := cmd.Command(cmd.Args().First())
		if sub == nil {
			return cmd
		}
		cmd = sub
	}
}

// repairingConfig reports whether a config write runs past err: a value sdd
// refuses to load is what `sdd config set` and `unset` repair, and the
// handler still refuses writing one.
func repairingConfig(configWrite bool, err error) bool {
	return configWrite && model.IsRefusedConfigValue(err)
}

// repointsProject reports whether root runs `sdd config set|unset --local
// project_dir`, which writes the work repository's own local file and so runs
// past a project the pointer fails to name (configWriteLayer).
func repointsProject(root *cli.Command) bool {
	if !writesConfig(root) {
		return false
	}
	cmd := invokedCommand(root)
	return cmd.Bool("local") && cmd.Args().First() == "project_dir"
}

// effectiveConfigResult builds the provenance view shared by the bare
// command and `get`. Outside an sdd repo the per-repo layers are simply
// absent — global settings and defaults still resolve.
func effectiveConfigResult(key string) (*query.EffectiveConfigResult, error) {
	f, err := newReadFinder()
	if err != nil {
		return nil, err
	}
	sddDir, err := optionalSDDDir()
	if err != nil {
		return nil, err
	}
	localLayer, err := localConfigLayer()
	if err != nil {
		return nil, err
	}
	q := query.EffectiveConfigQuery{SDDDir: sddDir, LocalConfigPath: localLayer, Key: key}
	if sddDir != "" {
		project, err := resolveProject()
		if err != nil {
			return nil, err
		}
		if project.Pointed() {
			q.WorkSDDDir = meta.SDDDir(project.WorkRoot)
			workLocal := meta.LocalConfigPath(q.WorkSDDDir, "")
			switch {
			case project.GraphRepo == "" && localConfigOverride == "":
				// Each layer under its own name: the project's local, then the
				// work repository's over it.
				q.LocalConfigPath, q.WorkLocalConfigPath, q.WorkLocalOver = "", workLocal, true
			case workLocal != meta.LocalConfigPath(sddDir, localLayer):
				q.WorkLocalConfigPath = workLocal
			}
		}
	}
	return f.EffectiveConfig(q)
}

func runEffectiveConfig(cmd *cli.Command, key string) error {
	result, err := effectiveConfigResult(key)
	if err != nil {
		return err
	}
	if cmd.String("format") == "json" {
		enc := json.NewEncoder(cmd.Writer)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Entries)
	}
	if err := printConfigEntries(cmd.Writer, result.Entries); err != nil {
		return err
	}
	// The effective table answers "what will happen", and a setting that does
	// nothing is part of that answer.
	if key == "" {
		return printUnknownConfigKeys(cmd.ErrWriter)
	}
	return nil
}

func printUnknownConfigKeys(errWriter io.Writer) error {
	f, err := newReadFinder()
	if err != nil {
		return err
	}
	sddDir, err := optionalSDDDir()
	if err != nil {
		return err
	}
	localLayer, err := localConfigLayer()
	if err != nil {
		return err
	}
	result, err := f.UnknownConfigKeys(query.UnknownConfigKeysQuery{SDDDir: sddDir, LocalConfigPath: localLayer})
	if err != nil {
		return err
	}
	if len(result.Keys) > 0 {
		fmt.Fprintf(errWriter, "\nignored — this sdd does not know these keys:\n")
		for _, k := range result.Keys {
			fmt.Fprintf(errWriter, "  %s  (%s)\n", k.Key, k.File)
		}
	}
	if len(result.Ignored) > 0 {
		fmt.Fprintf(errWriter, "\nignored — these settings are not honored where they stand:\n")
		for _, s := range result.Ignored {
			fmt.Fprintf(errWriter, "  %s  (%s): %s\n", s.Key, s.File, s.Reason)
		}
	}
	leftover, err := f.LeftoverRelevance(query.LeftoverRelevanceQuery{SDDDir: sddDir})
	if err != nil {
		return err
	}
	if len(leftover.Segments) > 0 {
		ledgerDir, err := workingRelevanceDir(sddDir)
		if err != nil {
			return err
		}
		fmt.Fprintf(errWriter, "\n%s:\n  %s  (%d segments): %s\n", leftoverRelevanceNotice, leftover.Dir, len(leftover.Segments), leftoverRelevanceAdvice(ledgerDir))
	}
	return nil
}

func runConfigGet(writer io.Writer, key string) error {
	result, err := effectiveConfigResult(key)
	if err != nil {
		return err
	}
	if len(result.Entries) == 0 {
		if key == "" {
			return fmt.Errorf("no configuration found")
		}
		return fmt.Errorf("no value for %q", key)
	}
	// An exact single-key hit prints the bare value (script-friendly);
	// a subtree or the whole config renders as the provenance table.
	if len(result.Entries) == 1 && result.Entries[0].Key == key {
		fmt.Fprintln(writer, result.Entries[0].Value)
		return nil
	}
	return printConfigEntries(writer, result.Entries)
}

// tableValueEscaper keeps multi-line values on one table row; the bare
// single-value output stays raw.
var tableValueEscaper = strings.NewReplacer("\n", `\n`, "\t", `\t`)

func printConfigEntries(writer io.Writer, entries []query.ConfigEntry) error {
	w := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t(%s)\n", e.Key, tableValueEscaper.Replace(e.Value), e.Source)
	}
	return w.Flush()
}

func runConfigSet(ctx context.Context, writer, errWriter io.Writer, set *command.ConfigSetCmd) error {
	// sddDir is optional for global writes — `sdd config set` must work
	// outside any repo (that is the point of the global layer).
	sddDir, localLayer, err := configWriteLayer(set.Target, set.Key)
	if err != nil {
		return err
	}
	_, mgr, err := defaultRepos()
	if err != nil {
		return err
	}
	h := handlers.New(handlers.Options{
		Stderr:          errWriter,
		SDDDir:          sddDir,
		LocalConfigPath: localLayer,
		Repos:           mgr,
	})
	err = h.ConfigSet(ctx, set)
	if err != nil {
		return err
	}
	if set.Repo != "" {
		fmt.Fprintf(writer, "%s = %s for %s (%s)\n", set.Key, set.Value, set.Repo, set.Target)
		return nil
	}
	fmt.Fprintf(writer, "%s = %s (%s)\n", set.Key, set.Value, set.Target)
	return nil
}

func runConfigUnset(ctx context.Context, writer, errWriter io.Writer, target, key string) error {
	// sddDir is optional for global writes, exactly as in runConfigSet.
	sddDir, localLayer, err := configWriteLayer(target, key)
	if err != nil {
		return err
	}
	_, mgr, err := defaultRepos()
	if err != nil {
		return err
	}
	h := handlers.New(handlers.Options{
		Stderr:          errWriter,
		SDDDir:          sddDir,
		LocalConfigPath: localLayer,
		Repos:           mgr,
	})
	if err := h.ConfigUnset(ctx, &command.ConfigUnsetCmd{Target: target, Key: key}); err != nil {
		return err
	}
	fmt.Fprintf(writer, "%s removed (%s)\n", key, target)
	return nil
}

// configWriteLayer names the .sdd/ directory and local-layer file a config
// write targets. project_dir is the one key of the work repository's own
// local file, so it bypasses both the project a pointer names and
// --local-config, and resolves no project a broken pointer fails to name.
func configWriteLayer(target, key string) (sddDir, localLayer string, err error) {
	if target != "local" || key != "project_dir" {
		sddDir, err = optionalSDDDir()
		if err != nil {
			return "", "", err
		}
		localLayer, err = localConfigLayer()
		return sddDir, localLayer, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("getting working directory: %w", err)
	}
	workRoot := meta.DiscoverRoot(cwd)
	if workRoot == "" {
		return "", "", errNoProject
	}
	workSDDDir := meta.SDDDir(workRoot)
	pointers, err := meta.ReadLayoutConfig(workSDDDir, "")
	if err != nil {
		return "", "", err
	}
	switch {
	case pointers != nil && pointers.ProjectDir != "":
		return workSDDDir, "", nil
	case pointers != nil && pointers.GraphRepo != "":
		return "", "", fmt.Errorf("%s points at its graph through graph_repo, which project_dir does not override", workRoot)
	default:
		return "", "", fmt.Errorf("%s holds a project of its own; point a work repository at a project with `sdd init --project-dir <path>`, then project_dir in its .sdd/config.local.yaml overrides the path for this machine", workRoot)
	}
}

package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// writeLocalSettings records the per-person settings cmd carries, the
// participant, the dialogue mode and the verifier runner, in sddDir's
// config.local.yaml, keeping every other key in that file.
func writeLocalSettings(sddDir string, cmd *command.InitCmd, changed *bool) error {
	settings := []struct{ key, value string }{
		{"participant", cmd.Participant},
		{"dialogue.mode", string(cmd.DialogueMode)},
		{"verify.runner", string(cmd.VerifierRunner)},
	}
	configLocalPath := meta.LocalConfigPath(sddDir, cmd.LocalConfigPath)
	for _, setting := range settings {
		if setting.value == "" {
			continue
		}
		existing, err := os.ReadFile(configLocalPath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("reading %s: %w", configLocalPath, err)
		}
		updated, err := model.SetYAMLField(existing, setting.key, setting.value)
		if err != nil {
			return fmt.Errorf("updating %s: %w", configLocalPath, err)
		}
		if bytes.Equal(existing, updated) {
			continue
		}
		err = os.WriteFile(configLocalPath, updated, 0o644)
		if err != nil {
			return fmt.Errorf("writing %s: %w", configLocalPath, err)
		}
		*changed = true
		// Not committed: config.local.yaml is gitignored, so staging it would
		// fail the commit.
		if setting.key == "participant" && cmd.OnParticipantWritten != nil {
			cmd.OnParticipantWritten(configLocalPath, setting.value)
		}
		if setting.key != "participant" && cmd.OnLocalSettingWritten != nil {
			cmd.OnLocalSettingWritten(configLocalPath, setting.key, setting.value)
		}
	}
	return nil
}

// persistSupportedAgents records an explicit --agents selection and prunes the
// renders of any agent it drops (d-tac-jin). Prune runs before the config
// write: a failed prune then leaves supported_agents still recording the
// dropped agent, so a re-run retries the drop rather than orphaning files under
// a config that already claims they're gone.
func (h *Handler) persistSupportedAgents(ctx context.Context, cmd *command.InitCmd, configPath string, recorded, effective []model.AgentTarget, scope model.Scope, touched *[]string) error {
	// Prune only under project scope, where renders are per-project and
	// committed. Under user scope the skill dirs are shared across every
	// project on the machine, so a per-project drop must not delete a render
	// another project relies on.
	if scope == model.ScopeProject {
		dropped := agentsDiff(recorded, effective)
		if len(dropped) > 0 {
			err := h.pruneAgentSkills(ctx, dropped, scope, cmd, touched)
			if err != nil {
				return err
			}
		}
	}
	// SetYAMLSequence re-serializes the file and drops its blank lines.
	if slices.Equal(recorded, effective) {
		return nil
	}
	existing, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", configPath, err)
	}
	updated, err := model.SetYAMLSequence(existing, "supported_agents", agentTargetsToStrings(effective))
	if err != nil {
		return fmt.Errorf("updating %s: %w", configPath, err)
	}
	if bytes.Equal(existing, updated) {
		return nil
	}
	err = os.WriteFile(configPath, updated, 0o644)
	if err != nil {
		return fmt.Errorf("writing %s: %w", configPath, err)
	}
	*touched = append(*touched, configPath)
	return nil
}

// installAgentSurfaces installs or refreshes the skill bundle, sweeps what the
// bundle stopped shipping, registers the MCP server, and commits everything
// touched inside cmd.RepoRoot, setting *changed when it touched or committed
// anything.
func (h *Handler) installAgentSurfaces(ctx context.Context, cmd *command.InitCmd, agents []model.AgentTarget, scope model.Scope, touched []string, commitMessage string, changed *bool) error {
	if !cmd.GraphOnly {
		var err error
		touched, err = h.installSkillsAndRegistrations(ctx, cmd, agents, scope, touched)
		if err != nil {
			return err
		}
	}
	*changed = *changed || len(touched) > 0
	return h.commitInitPaths(ctx, cmd, touched, commitMessage, changed)
}

func (h *Handler) installSkillsAndRegistrations(ctx context.Context, cmd *command.InitCmd, agents []model.AgentTarget, scope model.Scope, touched []string) ([]string, error) {
	for _, target := range agents {
		err := h.InstallSkills(ctx, &command.InstallSkillsCmd{
			Target:          target,
			Scope:           scope,
			RepoRoot:        cmd.RepoRoot,
			UserHome:        cmd.UserHome,
			BinaryVersion:   cmd.BinaryVersion,
			Force:           cmd.Force,
			PromptOverwrite: cmd.PromptOverwrite,
			OnInstalled: func(r command.SkillInstallResult) {
				touched = append(touched, r.Installed...)
				touched = append(touched, r.Refreshed...)
				touched = append(touched, r.Overwritten...)
				if cmd.OnSkillsInstalled != nil {
					cmd.OnSkillsInstalled(r)
				}
			},
		})
		if err != nil {
			return nil, fmt.Errorf("installing skills for %s: %w", target, err)
		}
	}

	// After the install pass so this run's own writes are on disk and stamped
	// (s-tac-zaz).
	err := h.pruneSkillOrphans(ctx, agents, scope, cmd, &touched)
	if err != nil {
		return nil, err
	}

	// Project scope only: the registration files live in the repo tree;
	// user-scope registration (home-dir config) is deferred (d-tac-wfl).
	if scope == model.ScopeProject {
		err = h.registerMCPServers(agents, cmd, &touched)
		if err != nil {
			return nil, err
		}
	}
	return touched, nil
}

// commitInitPaths commits everything init touched inside cmd.RepoRoot.
func (h *Handler) commitInitPaths(ctx context.Context, cmd *command.InitCmd, touched []string, commitMessage string, changed *bool) error {
	// Skill files installed under the user-global scope sit outside the repo's
	// git tree.
	if h.committer == nil {
		return nil
	}
	repoRoot, err := filepath.Abs(cmd.RepoRoot)
	if err != nil {
		return fmt.Errorf("resolving the repository root: %w", err)
	}
	record := uncommittedRecord{repoRoot: repoRoot}
	leftover, err := record.read()
	if err != nil {
		return err
	}
	commitPaths := filterRepoPaths(repoRoot, touched)
	if h.tracker != nil {
		commitPaths, err = h.tracker.Committable(ctx, commitPaths)
		if err != nil {
			return fmt.Errorf("selecting init paths to commit: %w", err)
		}
		// A file init rewrote identical to HEAD has nothing to commit (d-tac-sdq).
		commitPaths, err = h.tracker.Uncommitted(ctx, commitPaths)
		if err != nil {
			return fmt.Errorf("selecting init paths that differ from HEAD: %w", err)
		}
		leftover, err = h.tracker.Uncommitted(ctx, leftover)
		if err != nil {
			return fmt.Errorf("selecting the paths an earlier init left uncommitted: %w", err)
		}
	}
	for _, path := range leftover {
		if !slices.Contains(commitPaths, path) {
			commitPaths = append(commitPaths, path)
		}
	}
	if len(commitPaths) == 0 {
		return record.clear()
	}
	*changed = true
	err = h.commitGraph(ctx, commitMessage, commitPaths...)
	if err != nil {
		return errors.Join(fmt.Errorf("git commit, which the next sdd init retries: %w", err), record.write(commitPaths))
	}
	return record.clear()
}

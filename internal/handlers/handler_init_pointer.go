package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
)

// initPointer sets up a work repository whose graph lives in the sdd project
// at cmd.ProjectDir, running the project's own init first: a set-up when the
// project is missing, a refresh otherwise (20261029-132740-d-tac-g32). The
// project owns every setting, the graph, meta.json and runtime state, so this
// repository receives only the pointer config, the agents' skills and
// registrations, and the project's gitignore entries.
func (h *Handler) initPointer(ctx context.Context, cmd *command.InitCmd) error {
	if cmd.Bump {
		return fmt.Errorf("--bump raises the project's minimum_version; run it in %s", cmd.ProjectDir)
	}
	projectRoot := cmd.ProjectDir
	if !filepath.IsAbs(projectRoot) {
		projectRoot = filepath.Join(cmd.RepoRoot, projectRoot)
	}
	projectRoot = filepath.Clean(projectRoot)
	projectSDDDir := meta.SDDDir(projectRoot)
	projectCfg, _, err := meta.ReadConfigLayers(projectSDDDir)
	if err != nil {
		return err
	}
	switch {
	case projectCfg == nil && !cmd.SetUpProject:
		return fmt.Errorf("%s: %w; clone the project there, or set up a new one with `sdd init --project-dir %s`", cmd.ProjectDir, meta.ErrNotAProject, cmd.ProjectDir)
	case projectCfg != nil && (cmd.GraphDir != "" || cmd.Language != ""):
		return fmt.Errorf("--graph-dir and --language configure the project holding the graph; run `sdd init` with them in %s", cmd.ProjectDir)
	case projectCfg != nil && projectCfg.ProjectDir != "":
		return fmt.Errorf("%s sets project_dir itself; point at the project holding the graph", cmd.ProjectDir)
	}

	sddDir := filepath.Join(cmd.RepoRoot, model.SDDDirName)
	configPath := filepath.Join(sddDir, "config.yaml")
	existing, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", configPath, err)
	}
	configExisted := err == nil
	var recorded *model.PerRepoConfig
	if configExisted {
		var parseErr error
		recorded, parseErr = model.ParseConfig(existing)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", configPath, parseErr)
		}
		if recorded.ProjectDir == "" {
			return fmt.Errorf("%s configures a project of its own; reduce it to project_dir, skill_scope and supported_agents before pointing it at %s", configPath, cmd.ProjectDir)
		}
	}

	effectiveScope, persistScope, err := resolveSkillScope(configExisted, configPath, cmd.Scope, cmd.ScopeExplicit)
	if err != nil {
		return err
	}
	effectiveAgents, recordedAgents, persistAgents, err := resolveSupportedAgents(configExisted, configPath, cmd.Targets)
	if err != nil {
		return err
	}

	if projectCfg == nil {
		err = h.setUpProject(ctx, cmd, projectRoot, effectiveScope, effectiveAgents)
	} else {
		err = h.refreshProject(ctx, cmd, projectRoot, projectCfg)
	}
	if err != nil {
		return err
	}
	projectCfg, _, err = meta.ReadConfigLayers(projectSDDDir)
	if err != nil {
		return err
	}

	var touched []string
	if !configExisted {
		err = os.MkdirAll(sddDir, 0o755)
		if err != nil {
			return fmt.Errorf("creating %s: %w", sddDir, err)
		}
		err = os.WriteFile(configPath, []byte(model.FormatPointerConfig(model.PerRepoConfig{ProjectDir: cmd.ProjectDir, SkillScope: effectiveScope, SupportedAgents: effectiveAgents})), 0o644)
		if err != nil {
			return fmt.Errorf("writing %s: %w", configPath, err)
		}
		touched = append(touched, configPath)
		if cmd.OnCreated != nil {
			cmd.OnCreated(sddDir, meta.ResolveGraphDir(filepath.Dir(projectSDDDir), projectCfg))
		}
	} else {
		// SetYAMLField re-serializes the file, so it runs only for a value that
		// changes: a repeat init leaves the file byte-identical.
		updated := existing
		var setErr error
		if recorded.ProjectDir != cmd.ProjectDir {
			updated, setErr = model.SetYAMLField(updated, "project_dir", cmd.ProjectDir)
			if setErr != nil {
				return fmt.Errorf("updating %s: %w", configPath, setErr)
			}
		}
		if persistScope {
			updated, setErr = model.SetYAMLField(updated, "skill_scope", string(effectiveScope))
			if setErr != nil {
				return fmt.Errorf("updating %s: %w", configPath, setErr)
			}
		}
		if !bytes.Equal(existing, updated) {
			err = os.WriteFile(configPath, updated, 0o644)
			if err != nil {
				return fmt.Errorf("writing %s: %w", configPath, err)
			}
			touched = append(touched, configPath)
		}
		if persistAgents {
			err = h.persistSupportedAgents(ctx, cmd, configPath, recordedAgents, effectiveAgents, effectiveScope, &touched)
			if err != nil {
				return err
			}
		}
	}

	gitignorePath := filepath.Join(cmd.RepoRoot, ".gitignore")
	projectGraphDir := projectCfg.GraphDir
	if projectGraphDir == "" {
		projectGraphDir = model.DefaultGraphDir
	}
	gitignoreAdded, err := ensureGitignoreEntries(gitignorePath, initGitignoreEntries(projectGraphDir))
	if err != nil {
		return fmt.Errorf("updating %s: %w", gitignorePath, err)
	}
	if gitignoreAdded {
		touched = append(touched, gitignorePath)
		if cmd.OnGitignoreUpdated != nil {
			cmd.OnGitignoreUpdated(gitignorePath)
		}
	}

	bridgePaths, err := scaffoldInstructionBridge(cmd.RepoRoot, effectiveAgents)
	if err != nil {
		return err
	}
	if len(bridgePaths) > 0 {
		touched = append(touched, bridgePaths...)
		if cmd.OnBridgeScaffolded != nil {
			cmd.OnBridgeScaffolded(bridgePaths)
		}
	}

	return h.installAgentSurfaces(ctx, cmd, effectiveAgents, effectiveScope, touched, initCommitMessage(configExisted))
}

// writeLocalSettings records the per-person settings cmd carries, the
// participant and graph sync, in sddDir's config.local.yaml, keeping every
// other key in that file.
func writeLocalSettings(sddDir string, cmd *command.InitCmd) error {
	settings := []struct{ key, value string }{
		{"participant", cmd.Participant},
		{"sync.mode", string(cmd.SyncMode)},
		{"sync.interval", cmd.SyncInterval},
	}
	configLocalPath := filepath.Join(sddDir, "config.local.yaml")
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
// touched inside cmd.RepoRoot.
func (h *Handler) installAgentSurfaces(ctx context.Context, cmd *command.InitCmd, agents []model.AgentTarget, scope model.Scope, touched []string, commitMessage string) error {
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
			return fmt.Errorf("installing skills for %s: %w", target, err)
		}
	}

	// After the install pass so this run's own writes are on disk and stamped
	// (s-tac-zaz).
	err := h.pruneSkillOrphans(ctx, agents, scope, cmd, &touched)
	if err != nil {
		return err
	}

	// Project scope only: the registration files live in the repo tree;
	// user-scope registration (home-dir config) is deferred (d-tac-wfl).
	if scope == model.ScopeProject {
		err = h.registerMCPServers(agents, cmd, &touched)
		if err != nil {
			return err
		}
	}

	// Skill files installed under the user-global scope sit outside the repo's
	// git tree.
	if h.committer == nil {
		return nil
	}
	commitPaths := filterRepoPaths(cmd.RepoRoot, touched)
	if len(commitPaths) == 0 {
		return nil
	}
	err = h.commitGraph(ctx, commitMessage, commitPaths...)
	if err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}

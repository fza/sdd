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

// initGraphRepo sets up a code repository whose graph lives in the graph
// repository cmd.GraphRepo (20261106-202055-d-tac-eyl): the graph is cloned
// into sdd's own clone, set up or refreshed there and pushed; the code
// repository receives graph_repo, the stock guard and the agent surfaces in
// one commit.
func (h *Handler) initGraphRepo(ctx context.Context, cmd *command.InitCmd) error {
	if cmd.Bump {
		return fmt.Errorf("--bump raises the graph repository's minimum_version; run it where %s is checked out", cmd.GraphRepo)
	}
	if h.graphClones == nil || h.projectRepos == nil || h.repositoryAt == nil {
		return errors.New("graph_repo needs sdd's graph clones and a git adapter")
	}
	sddDir := filepath.Join(cmd.RepoRoot, model.SDDDirName)
	configPath := filepath.Join(sddDir, "config.yaml")
	existing, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", configPath, err)
	}
	configExisted := err == nil
	if configExisted {
		recorded, parseErr := model.ParseConfig(existing)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", configPath, parseErr)
		}
		if recorded.GraphRepo == "" && recorded.ProjectDir == "" {
			return fmt.Errorf("%s configures a project of its own; reduce it to the pointer keys before pointing it at %s", configPath, cmd.GraphRepo)
		}
	}
	scope, persistScope, err := resolveSkillScope(configExisted, configPath, cmd.Scope, cmd.ScopeExplicit)
	if err != nil {
		return err
	}
	agents, recordedAgents, persistAgents, err := resolveSupportedAgents(configExisted, configPath, cmd.Targets)
	if err != nil {
		return err
	}

	err = h.EnsureGraphClone(ctx, &command.EnsureGraphCloneCmd{RepoID: cmd.GraphRepo, CodeOrigin: cmd.RemoteURL})
	if err != nil {
		return err
	}
	cloneDir, err := h.graphClones.Dir(cmd.GraphRepo)
	if err != nil {
		return err
	}
	projectCfg, _, err := meta.ReadConfigLayers(meta.SDDDir(cloneDir), "")
	if err != nil {
		return err
	}
	if projectCfg != nil && (cmd.GraphDir != "" || cmd.Language != "") {
		return fmt.Errorf("--graph-dir and --language configure the graph repository %s, which is set up already", cmd.GraphRepo)
	}
	_, httpsURL, err := model.GraphRemoteURLs(cmd.GraphRepo)
	if err != nil {
		return err
	}
	branch := ""
	if projectCfg == nil || projectCfg.DefaultBranch == "" {
		branch, err = h.projectRepos.CurrentBranch(cloneDir)
		if err != nil {
			return err
		}
	}
	localConfigPath := meta.LocalConfigPath(sddDir, cmd.LocalConfigPath)
	err = os.MkdirAll(filepath.Dir(localConfigPath), 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(localConfigPath), err)
	}
	err = h.runProjectInit(ctx, cmd, cloneDir, func(projectCmd *command.InitCmd) {
		projectCmd.LocalConfigPath = localConfigPath
		projectCmd.RemoteURL = httpsURL
		projectCmd.GraphOnly = true
		projectCmd.DefaultBranch = branch
		if projectCfg == nil {
			if projectCmd.GraphDir == "" {
				projectCmd.GraphDir = model.DefaultGraphDirFor(true)
			}
			projectCmd.Scope = scope
			projectCmd.ScopeExplicit = true
			projectCmd.Targets = agents
			return
		}
		projectCmd.GraphDir = projectCfg.GraphDir
		projectCmd.Scope = ""
		projectCmd.ScopeExplicit = false
		projectCmd.Targets = nil
	})
	if err != nil {
		return err
	}
	err = h.graphClones.Publish(ctx, cmd.GraphRepo)
	if err != nil {
		return err
	}

	var touched []string
	if !configExisted {
		err = os.MkdirAll(sddDir, 0o755)
		if err != nil {
			return fmt.Errorf("creating %s: %w", sddDir, err)
		}
		pointer := model.PerRepoConfig{GraphRepo: cmd.GraphRepo, SkillScope: scope, SupportedAgents: agents}
		err = os.WriteFile(configPath, []byte(model.FormatGraphRepoPointerConfig(pointer)), 0o644)
		if err != nil {
			return fmt.Errorf("writing %s: %w", configPath, err)
		}
		touched = append(touched, configPath)
		if cmd.OnCreated != nil {
			cmd.OnCreated(sddDir, cloneDir)
		}
	} else {
		updated := existing
		settings := []struct{ key, value string }{{"graph_repo", string(cmd.GraphRepo)}, {"graph_dir", model.GraphElsewhereFile}}
		if persistScope {
			settings = append(settings, struct{ key, value string }{"skill_scope", string(scope)})
		}
		recorded, _ := model.ParseConfig(existing)
		current := map[string]string{"graph_repo": string(recorded.GraphRepo), "graph_dir": recorded.GraphDir, "skill_scope": string(recorded.SkillScope)}
		for _, setting := range settings {
			if current[setting.key] == setting.value {
				continue
			}
			updated, err = model.SetYAMLField(updated, setting.key, setting.value)
			if err != nil {
				return fmt.Errorf("updating %s: %w", configPath, err)
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
			err = h.persistSupportedAgents(ctx, cmd, configPath, recordedAgents, agents, scope, &touched)
			if err != nil {
				return err
			}
		}
	}
	gitignorePath := filepath.Join(cmd.RepoRoot, ".gitignore")
	gitignoreAdded, err := ensureGitignoreEntries(gitignorePath, []string{".sdd/" + meta.LocalConfigFileName})
	if err != nil {
		return fmt.Errorf("updating %s: %w", gitignorePath, err)
	}
	if gitignoreAdded {
		touched = append(touched, gitignorePath)
		if cmd.OnGitignoreUpdated != nil {
			cmd.OnGitignoreUpdated(gitignorePath)
		}
	}
	guardPath := filepath.Join(cmd.RepoRoot, filepath.FromSlash(model.GraphElsewhereFile))
	guard := model.GraphElsewhereText(cmd.GraphRepo)
	current, err := os.ReadFile(guardPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", guardPath, err)
	}
	if string(current) != guard {
		err = os.WriteFile(guardPath, []byte(guard), 0o644)
		if err != nil {
			return fmt.Errorf("writing %s: %w", guardPath, err)
		}
		touched = append(touched, guardPath)
	}
	return h.installAgentSurfaces(ctx, cmd, agents, scope, touched, initCommitMessage(configExisted))
}

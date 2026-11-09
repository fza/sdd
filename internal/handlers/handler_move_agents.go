package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// installPlan is what init's install step writes in the repository at root
// for agents under scope: the skills it renders and the MCP registrations,
// as files of a move commit. Only project scope keeps them in the
// repository. A skill edited since sdd wrote it is left as init leaves it, and
// a surface skip lists is left alone.
func (h *Handler) installPlan(ctx context.Context, root string, scope model.Scope, agents []model.AgentTarget, skip model.InitConfig, binaryVersion string) ([]model.MoveFile, error) {
	if scope != model.ScopeProject {
		return nil, nil
	}
	if h.reader == nil {
		return nil, errors.New("installing the agent surfaces needs the skill status reader")
	}
	var files []model.MoveFile
	add := func(abs string, content []byte) error {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		files = append(files, model.MoveFile{Path: rel, Content: content})
		return nil
	}
	for _, agent := range agents {
		if !skip.Skips(model.InitSurfaceSkills) {
			status, err := h.reader.SkillStatus(ctx, query.SkillStatusQuery{Target: agent, Scope: scope, RepoRoot: root})
			if err != nil {
				return nil, fmt.Errorf("reading the %s skills in %s: %w", agent, root, err)
			}
			for _, entry := range status.Entries {
				if entry.Status != model.SkillStatusMissing && entry.Status != model.SkillStatusPristine {
					continue
				}
				rendered, err := model.RenderSkillFile(entry.Embedded, binaryVersion, model.ComputeSkillHash(entry.Embedded.Content))
				if err != nil {
					return nil, fmt.Errorf("rendering %s: %w", entry.AbsPath, err)
				}
				err = add(entry.AbsPath, rendered)
				if err != nil {
					return nil, err
				}
			}
		}
		targets, err := model.MCPRegistrationTargets(agent, root)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			if skip.Skips(target.Surface) {
				continue
			}
			existing, err := readOptionalFile(target.Path)
			if err != nil {
				return nil, err
			}
			merged, changed, err := target.Merge(existing)
			if err != nil {
				return nil, fmt.Errorf("registering the MCP server in %s: %w", target.Path, err)
			}
			if changed {
				err = add(target.Path, merged)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return files, nil
}

// workInstallPlan is installPlan for the repository at root under the skill
// scope and agents the code repository at workRoot records.
func (h *Handler) workInstallPlan(ctx context.Context, workRoot, root, binaryVersion string) ([]model.MoveFile, error) {
	config, err := os.ReadFile(filepath.Join(workRoot, model.SDDDirName, "config.yaml"))
	if err != nil {
		return nil, fmt.Errorf("reading this repository's config: %w", err)
	}
	recorded, err := model.ParseConfig(config)
	if err != nil {
		return nil, fmt.Errorf("parsing this repository's config: %w", err)
	}
	scope := recorded.SkillScope
	if scope == "" {
		scope = model.DefaultScope
	}
	agents := recorded.SupportedAgents
	if len(agents) == 0 {
		agents = model.DefaultSupportedAgents
	}
	return h.installPlan(ctx, root, scope, agents, recorded.Init, binaryVersion)
}

// vacatePlan is what removes from the repository at root the agent surfaces
// init installed there for the project its config records: every skill sdd
// wrote and the MCP registrations, as files of a move commit. A skill whose
// content no longer matches its stamp refuses the move, naming the file.
func (h *Handler) vacatePlan(ctx context.Context, root string) ([]model.MoveFile, error) {
	config, err := readOptionalFile(filepath.Join(root, model.SDDDirName, "config.yaml"))
	if err != nil || config == nil {
		return nil, err
	}
	recorded, err := model.ParseConfig(config)
	if err != nil {
		return nil, fmt.Errorf("parsing the config in %s: %w", root, err)
	}
	scope := recorded.SkillScope
	if scope == "" {
		scope = model.DefaultScope
	}
	if scope != model.ScopeProject {
		return nil, nil
	}
	if h.reader == nil {
		return nil, errors.New("removing the agent surfaces needs the skill status reader")
	}
	agents := recorded.SupportedAgents
	if len(agents) == 0 {
		agents = model.DefaultSupportedAgents
	}
	var files []model.MoveFile
	remove := func(abs string) error {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return err
		}
		files = append(files, model.MoveFile{Path: rel, Remove: true})
		return nil
	}
	for _, agent := range agents {
		status, err := h.reader.SkillStatus(ctx, query.SkillStatusQuery{Target: agent, Scope: scope, RepoRoot: root})
		if err != nil {
			return nil, fmt.Errorf("reading the %s skills in %s: %w", agent, root, err)
		}
		for _, entry := range status.Entries {
			switch entry.Status {
			case model.SkillStatusMissing:
				continue
			case model.SkillStatusModified:
				return nil, editedSkill(entry.AbsPath)
			}
			err = remove(entry.AbsPath)
			if err != nil {
				return nil, err
			}
		}
		for _, orphan := range status.Orphans {
			if orphan.Class == model.SkillOrphanModified {
				return nil, editedSkill(orphan.AbsPath)
			}
			err = remove(orphan.AbsPath)
			if err != nil {
				return nil, err
			}
		}
		targets, err := model.MCPRegistrationTargets(agent, root)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			existing, err := readOptionalFile(target.Path)
			if err != nil {
				return nil, err
			}
			rest, changed, err := target.Remove(existing)
			if err != nil {
				return nil, fmt.Errorf("removing the MCP registration from %s: %w", target.Path, err)
			}
			if !changed {
				continue
			}
			if rest == nil {
				err = remove(target.Path)
			} else {
				var rel string
				rel, err = filepath.Rel(root, target.Path)
				files = append(files, model.MoveFile{Path: rel, Content: rest})
			}
			if err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}

func editedSkill(path string) error {
	return fmt.Errorf("%s no longer matches its sdd-content-hash stamp; the move removes sdd's skills from the checkout the graph leaves: restore the file or move the edit elsewhere, then run the move again; nothing was changed", path)
}

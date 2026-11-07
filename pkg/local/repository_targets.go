package local

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/syncstate"
	app "github.com/networkteam/sdd/pkg/application"
)

// NewRepositoryTargets opens configured graph readers and Git-backed mutation
// targets. Each acquisition rereads the global, committed and local config;
// a non-empty localConfigPath stands in for every checkout's
// .sdd/config.local.yaml. The git network operations sync starts pass a gate
// keeping its record in stateDir, under the limits the global config sets as
// read now (20261107-095427-d-tac-yux).
func NewRepositoryTargets(ctx context.Context, project app.ProjectID, serverCheckout, globalConfigPath, localConfigPath, stateDir string) (*GitWorktreeAcquirer, error) {
	limitsConfig, err := repos.LoadConfigFrom(globalConfigPath)
	if err != nil {
		return nil, err
	}
	gate := gitgate.New(gitgate.Options{StateDir: stateDir, Limits: repos.GitLimitsFunc(ctx, limitsConfig, globalConfigPath)})
	resolveConfig := func(checkout string) (*model.PerRepoConfig, error) {
		global, err := repos.LoadConfigFrom(globalConfigPath)
		if err != nil {
			return nil, err
		}
		cfg, err := meta.ResolveConfig(global.BaseConfig, filepath.Join(checkout, model.SDDDirName), localConfigPath)
		if err != nil {
			return nil, err
		}
		if cfg == nil {
			return nil, fmt.Errorf("checkout %q has no SDD configuration", checkout)
		}
		id := app.ProjectID(cfg.RepoID)
		if id == "" {
			id = "local"
		}
		if id != project {
			return nil, fmt.Errorf("checkout %q does not contain project %s", checkout, project)
		}
		return cfg, nil
	}
	acquirer, err := NewGitWorktreeAcquirer(GitWorktreeAcquirerOptions{
		Project: project, ServerCheckout: serverCheckout,
		ReadFactory: func(ctx context.Context, checkout string, q app.SnapshotReadQuery) (*app.AcquiredSnapshot, error) {
			cfg, err := resolveConfig(checkout)
			if err != nil {
				return nil, err
			}
			graph, err := NewFilesystemGraphStore(FilesystemGraphStoreOptions{
				Project: project, GraphDir: meta.ResolveGraphDir(checkout, cfg), Branch: q.Branch,
				MetaPath: filepath.Join(meta.SDDDir(checkout), model.SchemaMetaFileName), Contribution: cfg.Contribution,
			})
			if err != nil {
				return nil, err
			}
			return graph.AcquireSnapshot(ctx, q)
		},
		Factory: func(ctx context.Context, checkout string, target app.MutationTarget) (app.GraphStore, []app.MutationFinalizer, func() error, error) {
			cfg, err := resolveConfig(checkout)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("mutation target %s: %w", target.Branch, err)
			}
			if cfg.Contribution != "" {
				err = git.CLI{Dir: checkout}.CheckContribution(ctx, cfg.Contribution, cfg.DefaultBranch)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("mutation target %s: %w", target.Branch, err)
				}
			}
			graphDir := cfg.GraphDir
			if graphDir == "" {
				graphDir = model.DefaultGraphDir
			}
			finalizer := GitFinalizer{Checkout: checkout, GraphDir: graphDir, Branch: target.Branch}
			graph, err := NewFilesystemGraphStore(FilesystemGraphStoreOptions{
				Project: project, GraphDir: meta.ResolveGraphDir(checkout, cfg), Branch: target.Branch, PublicationGit: &finalizer,
				MetaPath: filepath.Join(meta.SDDDir(checkout), model.SchemaMetaFileName),
			})
			if err != nil {
				return nil, nil, nil, err
			}
			finalizers := []app.MutationFinalizer{finalizer}
			if syncstate.Mode(checkout) == model.SyncModeAuto {
				finalizers = append(finalizers, GraphPublisher{GraphDir: meta.ResolveGraphDir(checkout, cfg), gate: gate})
			}
			finalizers = append(finalizers, MergeDriverSetup{Checkout: checkout})
			return graph, finalizers, func() error { return nil }, nil
		},
	})
	if err != nil {
		return nil, err
	}
	acquirer.resolveConfig = resolveConfig
	acquirer.gate = gate
	return acquirer, nil
}

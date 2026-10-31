package cliapp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
	localadapter "github.com/networkteam/sdd/pkg/local"
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

func serveCmd() *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "Run the workflow MCP server: dialogue-loop tools over stdio or HTTP",
		Flags: append(embeddingFlags(),
			&cli.StringFlag{
				Name:  "transport",
				Value: "stdio",
				Usage: "transport: stdio or http",
			},
			&cli.StringFlag{
				Name:  "addr",
				Value: "127.0.0.1:8765",
				Usage: "HTTP listen address (transport=http only)",
			},
			&cli.StringFlag{
				Name:    "auth-token",
				Usage:   "bearer token required on every HTTP request (transport=http only)",
				Sources: cli.EnvVars("SDD_SERVE_TOKEN"),
			},
		),
		Action: withWriteGate(func(ctx context.Context, cmd *cli.Command) error {
			dir, err := resolveGraphDir(cmd)
			if err != nil {
				return err
			}
			sddDir, err := resolveSDDDir()
			if err != nil {
				return err
			}
			runner, err := newRunner(cmd, "")
			if err != nil {
				return err
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			reg, _, err := defaultRepos()
			if err != nil {
				return err
			}
			ctx = handlers.WithoutSyncReport(ctx)
			application, runtime, identity, err := buildLocalApplication(ctx, cmd, dir, sddDir, cfg, reg, runner)
			if err != nil {
				return err
			}
			timerCtx, stopTimer := context.WithCancel(ctx)
			timerDone := make(chan struct{})
			go func() {
				defer close(timerDone)
				err := runtime.RunSyncTimer(timerCtx)
				if err != nil {
					slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: timer: %v", err))
				}
			}()
			// A round in flight finishes before serve returns, so no merge
			// outlives the process that held the sync lock.
			defer func() {
				stopTimer()
				<-timerDone
			}()
			retention, err := model.ResolveSessionRetention(cfg)
			if err != nil {
				return err
			}
			collectSessions(ctx, application, retention)

			transport := cmd.String("transport")
			srv, err := mcpserver.New(mcpserver.Options{SearchSyncMode: sdd.SearchSyncAll,
				Application:   application,
				LocalIdentity: identity,
				LocalClient:   transport == "stdio",
				Version:       cmd.Root().Version,
			})
			if err != nil {
				return err
			}

			switch transport {
			case "stdio":
				return srv.Run(ctx, commandTransport(cmd.Reader, cmd.Writer))
			case "http":
				token := cmd.String("auth-token")
				if token == "" {
					return fmt.Errorf("transport=http requires --auth-token (or SDD_SERVE_TOKEN) — the write path must not be open to anyone who can reach the address")
				}
				addr := cmd.String("addr")
				fmt.Fprintf(cmd.ErrWriter, "sdd serve: listening on http://%s (bearer token required)\n", addr)
				return runLocalHTTP(ctx, addr, token, srv)
			default:
				return fmt.Errorf("invalid transport %q: use stdio or http", cmd.String("transport"))
			}
		}),
	}
}

func commandTransport(reader io.Reader, writer io.Writer) *mcp.IOTransport {
	input, ok := reader.(io.ReadCloser)
	if !ok {
		input = io.NopCloser(reader)
	}
	output, ok := writer.(io.WriteCloser)
	if !ok {
		output = writerWithoutClose{writer}
	}
	return &mcp.IOTransport{Reader: input, Writer: output}
}

type writerWithoutClose struct{ io.Writer }

func (writerWithoutClose) Close() error { return nil }

func runLocalHTTP(ctx context.Context, addr, token string, app *mcpserver.Server) error {
	httpServer := &http.Server{
		Addr:    addr,
		Handler: localBearerAuth(token, app.Handler()),
		BaseContext: func(net.Listener) context.Context {
			return context.WithoutCancel(ctx)
		},
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()

	select {
	case <-ctx.Done():
		return shutdownLocalHTTP(httpServer, app)
	case err := <-errCh:
		shutdownErr := shutdownMCPApp(app)
		if errors.Is(err, http.ErrServerClosed) {
			return shutdownErr
		}
		return errors.Join(err, shutdownErr)
	}
}

func shutdownLocalHTTP(httpServer *http.Server, app *mcpserver.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- httpServer.Shutdown(ctx) }()
	go func() { results <- app.Shutdown(ctx) }()

	var errs []error
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				errs = append(errs, err)
			}
		case <-ctx.Done():
			errs = append(errs, context.Cause(ctx))
			if err := httpServer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("force-closing HTTP transport: %w", err))
			}
			return errors.Join(errs...)
		}
	}
	return errors.Join(errs...)
}

func shutdownMCPApp(app *mcpserver.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return app.Shutdown(ctx)
}

func localBearerAuth(token string, next http.Handler) http.Handler {
	expect := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expect)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// localRuntimeAccess serves one repository and every connected repo its
// dependency closure reaches. A dependency resolves to one runtime built on
// demand from its cache, both as a member of a horizon and as a project a
// move may work in: its own declared dependencies, its own index namespace,
// no write authority until the ephemeral clone of d-cpt-n8z exists
// (d-cpt-yjc). ListProjects names the home alone, so a session still opens
// there without choosing.
type localRuntimeAccess struct {
	project      sdd.ProjectID
	participant  string
	dialogue     sdd.Dialogue
	runtime      *sdd.ProjectRuntime
	dependencies *cachedDependencies
}

// cachedDependencies builds a connected repo's runtime from its cache on
// first use and keeps it; a repo that is not connected or not cached is
// unavailable until it is. closure walks the served project's declarations
// over the same caches.
type cachedDependencies struct {
	registry *repos.Registry
	closure  *finders.Finder
	build    func(repos.Cache, *model.PerRepoConfig) (*sdd.ProjectRuntime, error)

	mu       sync.Mutex
	runtimes map[sdd.RepoID]*sdd.ProjectRuntime
}

func (d *cachedDependencies) resolve(repoID sdd.RepoID) (*sdd.ProjectRuntime, error) {
	if d == nil {
		return nil, dependencyUnavailable()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if runtime := d.runtimes[repoID]; runtime != nil {
		return runtime, nil
	}
	cache, connected, err := d.registry.Cache(repoID)
	if err != nil {
		return nil, err
	}
	if !connected || !cache.Cloned() {
		return nil, dependencyUnavailable()
	}
	cfg, err := cache.Config()
	if errors.Is(err, meta.ErrNotAnSDDProject) {
		cfg, err = &model.PerRepoConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	runtime, err := d.build(cache, cfg)
	if err != nil {
		return nil, err
	}
	if d.runtimes == nil {
		d.runtimes = map[sdd.RepoID]*sdd.ProjectRuntime{}
	}
	d.runtimes[repoID] = runtime
	return runtime, nil
}

// resolveMember resolves a project this process serves beyond its home: a
// member of the served project's closure (20261026-194528-d-cpt-gcq), never a
// repo merely connected on this machine.
func (d *cachedDependencies) resolveMember(ctx context.Context, repoID sdd.RepoID) (*sdd.ProjectRuntime, error) {
	if d == nil {
		return nil, dependencyUnavailable()
	}
	closure, err := d.closure.DependencyClosure(ctx, query.DependencyClosureQuery{})
	if err != nil {
		return nil, err
	}
	if !slices.Contains(closure, repoID) {
		return nil, dependencyUnavailable()
	}
	return d.resolve(repoID)
}

func dependencyUnavailable() error {
	return &sdd.ApplicationError{Code: sdd.ErrorProjectUnavailable, Message: "dependency unavailable"}
}

func (a *localRuntimeAccess) ResolvePrincipal(_ context.Context, identity sdd.RequestIdentity) (sdd.Principal, error) {
	if identity.Subject == "" {
		return sdd.Principal{}, &sdd.ApplicationError{Code: sdd.ErrorAuthenticationRequired, Message: "request identity is required"}
	}
	return sdd.Principal{Subject: identity.Subject}, nil
}

// ResolveParticipant returns the configured participant: one local process
// serves one project, so the name is the same for it.
func (a *localRuntimeAccess) ResolveParticipant(context.Context, sdd.Principal, sdd.ProjectID) (string, error) {
	return a.participant, nil
}

// ResolveDialogue returns the dialogue mode read from the person's config
// when the server started.
func (a *localRuntimeAccess) ResolveDialogue(context.Context, sdd.Principal, sdd.ProjectID) (sdd.Dialogue, error) {
	return a.dialogue, nil
}

func (a *localRuntimeAccess) ListProjects(context.Context, sdd.Principal) (sdd.ProjectList, error) {
	return sdd.ProjectList{Projects: []sdd.ProjectSummary{{ProjectRef: a.runtime.Project(), CanRead: true, CanWrite: true, State: sdd.ProjectReady}}}, nil
}

func (a *localRuntimeAccess) ResolveProject(ctx context.Context, _ sdd.Principal, project sdd.ProjectID, _ sdd.Access) (*sdd.ProjectRuntime, error) {
	if project == a.project {
		return a.runtime, nil
	}
	// Locally a dependency's project ID is its repo ID.
	return a.dependencies.resolveMember(ctx, sdd.RepoID(project))
}

func (a *localRuntimeAccess) AuthorizeSession(ctx context.Context, request sdd.SessionAccessRequest) error {
	return sdd.OwnerOnly(ctx, request)
}

func (a *localRuntimeAccess) ResolveDependency(_ context.Context, _ sdd.Principal, _ sdd.ProjectID, dependency sdd.RepoID) (*sdd.ProjectRuntime, error) {
	return a.dependencies.resolve(dependency)
}

// localDialogue reads dialogue.mode and the layer that sets it; the
// committed layer arrives without it (20261029-150854-d-tac-b2f).
func localDialogue(sddDir string) (sdd.Dialogue, error) {
	loc, err := repos.DefaultLocations()
	if err != nil {
		return sdd.Dialogue{}, err
	}
	global, err := repos.LoadConfigFrom(loc.ConfigPath)
	if err != nil {
		return sdd.Dialogue{}, err
	}
	committed, local, err := meta.ReadConfigLayers(sddDir, localConfigOverride)
	if err != nil {
		return sdd.Dialogue{}, err
	}
	sources := map[model.ConfigValueSource]string{
		model.ConfigSourceGlobal: loc.ConfigPath,
		model.ConfigSourceLocal:  meta.LocalConfigPath(sddDir, localConfigOverride),
	}
	for _, value := range model.EffectiveConfigValues(global.BaseConfig, committed, local) {
		if value.Key == "dialogue.mode" {
			return sdd.Dialogue{Mode: value.Value, Source: sources[value.Source]}, nil
		}
	}
	return sdd.Dialogue{Mode: string(model.DialogueGuided)}, nil
}

func buildLocalApplication(ctx context.Context, cmd *cli.Command, graphDir, sddDir string, cfg *model.PerRepoConfig, registry *repos.Registry, runner pkgllm.Runner) (*sdd.Application, *sdd.ProjectRuntime, sdd.RequestIdentity, error) {
	displayName := filepath.Base(filepath.Dir(sddDir))
	participant := ""
	language := ""
	var dependencies []sdd.RepoID
	if cfg != nil {
		participant = cfg.Participant
		language = cfg.Language
		dependencies = append(dependencies, cfg.Dependencies...)
	}
	locations, err := repos.DefaultLocations()
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	stableRepoRoot, err := git.StableRepoRoot(filepath.Dir(sddDir))
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	storeLocations, err := resolveSessionLocations(sddDir, cfg, locations)
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	project := sessionStoreProject(cfg)
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: project, GraphDir: graphDir, MetaPath: filepath.Join(sddDir, model.SchemaMetaFileName), Contribution: cfg.Contribution})
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	sessions, err := localadapter.NewFilesystemSessionStore(storeLocations...)
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStore(storeLocations...)
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	localEmbedder, err := buildEmbedder(cmd)
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	crossEmbedder, err := crossRepoEmbedder(cmd)
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	// The machine-global persistent store the CLI also builds and reads: one
	// store per (repo-key, embedding fingerprint) under the cache root. The
	// base repo key matches the CLI exactly (see cmd search resolveIndexStore);
	// the fingerprint selects the final StoreDir inside the adapter at request
	// time, so a lazy embedder that reveals its dimensionality only on first
	// use still routes correctly. No process-local memory store in production.
	cacheRoot := registry.CacheRoot()
	baseRepoKey := persistentIndexRepoKey(cfg, stableRepoRoot)
	baseIndex := localadapter.NewPersistentSearchIndexStore(project, cacheRoot, baseRepoKey)
	var embeddings embed.Embedder
	var runeCap func(context.Context) (int, error)
	if localEmbedder.Embedder != nil {
		embeddings = localEmbedder
		runeCap = localEmbedder.MaxRunes
	}
	serverCheckout := filepath.Dir(sddDir)
	targets, err := localadapter.NewRepositoryTargets(project, serverCheckout, locations.ConfigPath, localConfigOverride)
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	if cfg == nil || cfg.DefaultBranch == "" {
		return nil, nil, sdd.RequestIdentity{}, fmt.Errorf("default_branch is required in .sdd/config.yaml before serving mutation tools")
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: project, DisplayName: displayName}, DefaultBranch: cfg.DefaultBranch, Language: language,
		Dependencies: dependencies, Graph: localadapter.BranchReadStore{GraphStore: graph, Branches: targets, DefaultBranch: cfg.DefaultBranch}, Targets: targets, Sync: targets, Branches: targets, Base: targets,
		Embedder: embeddings, SearchIndex: optionalSearchIndex(embeddings, baseIndex), RuneCap: runeCap,
		LLM: runner,
	})
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	dialogue, err := localDialogue(sddDir)
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	var memberEmbedder embed.Embedder
	var memberRuneCap func(context.Context) (int, error)
	if crossEmbedder.Embedder != nil {
		memberEmbedder = crossEmbedder
		memberRuneCap = crossEmbedder.MaxRunes
	}
	access := &localRuntimeAccess{
		project: project, participant: participant, dialogue: dialogue, runtime: runtime,
		dependencies: &cachedDependencies{registry: registry, closure: finders.New(finders.Options{Config: cfg, Repos: registry}), build: func(cache repos.Cache, dependencyCfg *model.PerRepoConfig) (*sdd.ProjectRuntime, error) {
			// The cache as a project of its own: its committed config supplies
			// the graph layout, the language, and the dependencies its horizon
			// is composed from.
			graphDir, err := cache.GraphDir()
			if err != nil {
				return nil, err
			}
			memberGraph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: sdd.ProjectID(cache.RepoID), GraphDir: graphDir})
			if err != nil {
				return nil, err
			}
			// Connected stores are keyed by the connected repo's ID (the existing
			// connected-repository storage contract) and exclude embedded entries,
			// so binary-shipped base facts embed once per machine in the base
			// store, not once per connected repo.
			memberIndex := localadapter.NewPersistentSearchIndexStore(sdd.ProjectID(cache.RepoID), cacheRoot, index.RepoKey(cache.RepoID, cache.Dir))
			return sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
				Project: sdd.ProjectRef{ID: sdd.ProjectID(cache.RepoID), DisplayName: string(cache.RepoID)}, Graph: memberGraph,
				Language: dependencyCfg.Language, Dependencies: dependencyCfg.Dependencies,
				Embedder: memberEmbedder, SearchIndex: optionalSearchIndex(memberEmbedder, memberIndex), RuneCap: memberRuneCap, LLM: runner,
				ExcludeEmbeddedFromIndex: true,
			})
		}},
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: access, Sessions: sessions, StagedBlobs: blobs})
	if err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	identity := sdd.RequestIdentity{Subject: "local"}
	if _, err := application.Info(ctx, identity, project, sdd.InfoRequest{}); err != nil {
		return nil, nil, sdd.RequestIdentity{}, err
	}
	return application, runtime, identity, nil
}

// collectSessions runs one reclamation pass at startup. This is the trigger
// rather than a command because the store is being opened anyway and collection
// involves no procedure and no served instruction, so it stays host-neutral by
// construction.
//
// CollectSessions itself returns its error, so a composition calling it on a
// schedule can act on one. This adapter cannot: over stdio a server that exits
// leaves the agent with a dead pipe and no way to act, which is worse than the
// unreclaimed disk it would be reporting. So startup logs and serves.
func collectSessions(ctx context.Context, application *sdd.Application, retention time.Duration) {
	const pageSize = 200
	log := slogutils.FromContext(ctx)
	cmd := sdd.CollectSessionsCmd{Retention: retention, Limit: pageSize}
	var total sdd.CollectSessionsResult
	for {
		result, err := application.CollectSessions(ctx, cmd)
		if err != nil {
			log.Warn("session collection did not complete", "err", err, "after", cmd.After)
			return
		}
		total.RemovedSessions = append(total.RemovedSessions, result.RemovedSessions...)
		total.RemovedStaged = append(total.RemovedStaged, result.RemovedStaged...)
		total.Skipped = append(total.Skipped, result.Skipped...)
		if result.Next == "" {
			break
		}
		cmd.After = result.Next
	}
	if len(total.RemovedSessions) > 0 || len(total.RemovedStaged) > 0 {
		log.Info("collected session scaffolding",
			"sessions", len(total.RemovedSessions),
			"staged_sessions", len(total.RemovedStaged),
			"skipped", len(total.Skipped),
		)
	}
}

func optionalSearchIndex(embeddings embed.Embedder, index sdd.SearchIndexStore) sdd.SearchIndexStore {
	if embeddings == nil {
		return nil
	}
	return index
}

// repoIDOf returns the committed repo_id, or "" when unconfigured — the input
// to index.RepoKey, which hashes the repo root under the "local" namespace
// when there is no declared ID.
func repoIDOf(cfg *model.PerRepoConfig) model.RepoID {
	if cfg == nil {
		return ""
	}
	return cfg.RepoID
}

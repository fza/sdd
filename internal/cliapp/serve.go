package cliapp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/hooks"
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

func serveCmd(launchCachePrune func(dir, logPath string) error) *cli.Command {
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
			runner, err := newRunner(cmd, "")
			if err != nil {
				return err
			}
			ctx = handlers.WithoutSyncReport(ctx)
			watch, err := newConfigWatch(func() (string, error) { return resolveGraphDir(cmd) }, func() (*servedProject, error) {
				return buildServedProject(ctx, cmd, runner)
			})
			if err != nil {
				return err
			}
			watch.guards = append(watch.guards, (&strayGuard{}).check)
			application, identity, err := buildServedApplication(ctx, watch)
			if err != nil {
				return err
			}
			cfg := watch.served().cfg
			timerCtx, stopTimer := context.WithCancel(ctx)
			timerDone := make(chan struct{})
			go func() {
				defer close(timerDone)
				err := sdd.RunSyncTimerOf(timerCtx, watch.runtime)
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
			var cacheNotice func(context.Context) string
			if launchCachePrune != nil {
				go runCachePruneTimer(timerCtx, launchCachePrune)
				cacheNotice = serveCacheNotice
			}
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
				CacheNotice:   cacheNotice,
				BeforeCall:    watch.admit,
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
// dependency closure reaches. A dependency resolves to one runtime over its
// sdd-owned clone, both as a member of a horizon and as a project a move may
// work in, reads and captures alike: its own declared dependencies, its own
// index namespace, write authority only where both connected-write gates
// allow it (20261106-201216-d-cpt-aim). ListProjects names the home alone, so
// a session still opens there without choosing.
type localRuntimeAccess struct {
	project      sdd.ProjectID
	participant  string
	dialogue     sdd.Dialogue
	runtime      *sdd.ProjectRuntime
	dependencies *connectedDependencies
}

// connectedDependencies builds a connected repo's runtime from its
// sdd-owned clone on first use, making the clone when this machine holds
// none, and keeps it; a repo that is not connected, or whose first clone the
// git gate defers, is unavailable until it is. closure walks the served
// project's declarations over the same clones.
type connectedDependencies struct {
	registry *repos.Registry
	manager  *repos.Manager
	closure  *finders.Finder
	rounds   connectedRounds
	build    func(repoID sdd.RepoID, dir string, cfg *model.PerRepoConfig) (*sdd.ProjectRuntime, error)
	// writer and connected decide a capture into a dependency with the
	// dependency's committed inbound.accept (20261106-201216-d-cpt-aim).
	writer    sdd.RepoID
	connected model.ConnectedConfig

	mu       sync.Mutex
	runtimes map[sdd.RepoID]*sdd.ProjectRuntime
}

func (d *connectedDependencies) resolve(ctx context.Context, repoID sdd.RepoID) (*sdd.ProjectRuntime, error) {
	if d == nil {
		return nil, dependencyUnavailable()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if runtime := d.runtimes[repoID]; runtime != nil {
		return runtime, nil
	}
	global, err := d.registry.Load()
	if err != nil {
		return nil, err
	}
	repo, connected := global.Connected(repoID)
	if !connected {
		return nil, dependencyUnavailable()
	}
	dir, _, err := d.manager.Ensure(ctx, repo)
	var deferred *gitgate.DeferredError
	if errors.As(err, &deferred) {
		return nil, dependencyUnavailable()
	}
	if err != nil {
		return nil, err
	}
	cfg, err := repos.Clone{RepoID: repoID, Dir: dir}.Config()
	if errors.Is(err, meta.ErrNotAnSDDProject) {
		cfg, err = &model.PerRepoConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	runtime, err := d.build(repoID, dir, cfg)
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
func (d *connectedDependencies) resolveMember(ctx context.Context, repoID sdd.RepoID) (*sdd.ProjectRuntime, error) {
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
	return d.resolve(ctx, repoID)
}

// resolveWritable resolves a closure member a capture writes into: refused
// unless the writer's connected.write and the member's committed
// inbound.accept both allow the pair. The member's round runs first while
// due, so the gate reads inbound.accept as fresh as the git gate admits; a
// failed round leaves the clone as it stands (20261027-150208-d-tac-bg4).
func (d *connectedDependencies) resolveWritable(ctx context.Context, repoID sdd.RepoID) (*sdd.ProjectRuntime, error) {
	runtime, err := d.resolveMember(ctx, repoID)
	if err != nil {
		return nil, err
	}
	err = d.rounds.refresh(ctx, repoID)
	if err != nil {
		slogutils.FromContext(ctx).Warn(fmt.Sprintf("sync: refreshing %s before a capture: %v", repoID, err))
	}
	dir, err := d.registry.CloneDir(repoID)
	if err != nil {
		return nil, err
	}
	cfg, err := repos.Clone{RepoID: repoID, Dir: dir}.Config()
	if err != nil {
		return nil, err
	}
	err = model.ConnectedWriteRefusal(d.writer, d.connected, repoID, cfg.Inbound)
	if err != nil {
		return nil, &sdd.ApplicationError{Code: sdd.ErrorWriteDenied, Message: err.Error(), Cause: err}
	}
	return runtime, nil
}

// connectedRounds runs the sync rounds of the connected repos the served
// project reaches.
type connectedRounds struct {
	handler *handlers.Handler
}

func (c connectedRounds) SyncRepos(ctx context.Context, request sdd.SyncReposRequest) ([]sdd.RepoSyncOutcome, error) {
	var outcomes []sdd.RepoSyncOutcome
	cmd := &command.RepoSyncCmd{Due: request.Due, OnOutcome: func(outcome model.RepoSyncOutcome) {
		outcomes = append(outcomes, sdd.RepoSyncOutcome{Repo: sdd.RepoID(outcome.RepoID), State: sdd.RepoSyncState(outcome.State), Next: outcome.Next, Reason: outcome.Reason})
	}}
	for _, repo := range request.Repos {
		cmd.RepoIDs = append(cmd.RepoIDs, model.RepoID(repo))
	}
	err := c.handler.RepoSync(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return outcomes, nil
}

func (c connectedRounds) refresh(ctx context.Context, repoID sdd.RepoID) error {
	_, err := c.handler.EnsureReposFresh(ctx, command.EnsureReposFreshCmd{RepoIDs: []model.RepoID{model.RepoID(repoID)}})
	return err
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
// when this resolution of it was built.
func (a *localRuntimeAccess) ResolveDialogue(context.Context, sdd.Principal, sdd.ProjectID) (sdd.Dialogue, error) {
	return a.dialogue, nil
}

func (a *localRuntimeAccess) ListProjects(context.Context, sdd.Principal) (sdd.ProjectList, error) {
	return sdd.ProjectList{Projects: []sdd.ProjectSummary{{ProjectRef: a.runtime.Project(), CanRead: true, CanWrite: true, State: sdd.ProjectReady}}}, nil
}

func (a *localRuntimeAccess) ResolveProject(ctx context.Context, _ sdd.Principal, project sdd.ProjectID, access sdd.Access) (*sdd.ProjectRuntime, error) {
	if project == a.project {
		return a.runtime, nil
	}
	// Locally a dependency's project ID is its repo ID.
	if access == sdd.AccessWrite {
		return a.dependencies.resolveWritable(ctx, sdd.RepoID(project))
	}
	return a.dependencies.resolveMember(ctx, sdd.RepoID(project))
}

func (a *localRuntimeAccess) AuthorizeSession(ctx context.Context, request sdd.SessionAccessRequest) error {
	return sdd.OwnerOnly(ctx, request)
}

func (a *localRuntimeAccess) ResolveDependency(ctx context.Context, _ sdd.Principal, _ sdd.ProjectID, dependency sdd.RepoID) (*sdd.ProjectRuntime, error) {
	return a.dependencies.resolve(ctx, dependency)
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
	localLayer, err := localConfigLayer()
	if err != nil {
		return sdd.Dialogue{}, err
	}
	committed, local, err := meta.ReadConfigLayers(sddDir, localLayer)
	if err != nil {
		return sdd.Dialogue{}, err
	}
	sources := map[model.ConfigValueSource]string{
		model.ConfigSourceGlobal: loc.ConfigPath,
		model.ConfigSourceLocal:  meta.LocalConfigPath(sddDir, localLayer),
	}
	for _, value := range model.EffectiveConfigValues(global.BaseConfig, committed, local) {
		if value.Key == "dialogue.mode" {
			return sdd.Dialogue{Mode: value.Value, Source: sources[value.Source]}, nil
		}
	}
	return sdd.Dialogue{Mode: string(model.DialogueGuided)}, nil
}

// servedConfigLayers reads the config layers of the project at sddDir and
// names every file that sets them, existing or not: the user-global config,
// the project's committed and machine-local layer, and those of the
// repository pointing at it.
func servedConfigLayers(sddDir string) (model.ConfigLayers, []string, error) {
	loc, err := repos.DefaultLocations()
	if err != nil {
		return model.ConfigLayers{}, nil, err
	}
	global, err := repos.LoadConfigFrom(loc.ConfigPath)
	if err != nil {
		return model.ConfigLayers{}, nil, err
	}
	localLayer, err := localConfigLayer()
	if err != nil {
		return model.ConfigLayers{}, nil, err
	}
	committed, local, err := meta.ReadConfigLayers(sddDir, localLayer)
	if err != nil {
		return model.ConfigLayers{}, nil, err
	}
	layers := model.ConfigLayers{Global: global.BaseConfig, Project: committed, Local: local}
	watched := []string{loc.ConfigPath, filepath.Join(sddDir, "config.yaml"), meta.LocalConfigPath(sddDir, localLayer)}
	cwd, err := os.Getwd()
	if err != nil {
		return model.ConfigLayers{}, nil, fmt.Errorf("getting working directory: %w", err)
	}
	workRoot := meta.DiscoverRoot(cwd)
	if workRoot == "" {
		return layers, watched, nil
	}
	workSDDDir := meta.SDDDir(workRoot)
	watched = append(watched, filepath.Join(workSDDDir, "config.yaml"), meta.LocalConfigPath(workSDDDir, ""))
	if filepath.Clean(workSDDDir) != filepath.Clean(sddDir) {
		pointerCommitted, pointerLocal, err := meta.ReadConfigLayers(workSDDDir, "")
		if err != nil {
			return model.ConfigLayers{}, nil, err
		}
		layers.Pointer = model.MergeConfig(pointerCommitted, pointerLocal)
	}
	return layers, watched, nil
}

// buildServedApplication composes the application whose every call resolves
// against the project watch serves at that moment.
func buildServedApplication(ctx context.Context, watch *configWatch) (*sdd.Application, sdd.RequestIdentity, error) {
	application, err := sdd.NewApplication(sdd.ApplicationOptions{Access: servedAccess{watch}, Sessions: servedSessions{watch}, StagedBlobs: servedBlobs{watch}, DelegateLauncher: headlessLauncher{watch: watch}})
	if err != nil {
		return nil, sdd.RequestIdentity{}, err
	}
	identity := sdd.RequestIdentity{Subject: "local"}
	_, err = application.Info(ctx, identity, watch.served().access.project, sdd.InfoRequest{})
	if err != nil {
		return nil, sdd.RequestIdentity{}, err
	}
	return application, identity, nil
}

// buildServedProject resolves the project serve answers calls with from the
// config layers as they are now. ctx is the server's, which the connected
// dependencies the project builds later keep.
func buildServedProject(ctx context.Context, cmd *cli.Command, runner pkgllm.Runner) (*servedProject, error) {
	graphDir, err := resolveGraphDir(cmd)
	if err != nil {
		return nil, err
	}
	sddDir, err := resolveSDDDir()
	if err != nil {
		return nil, err
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	registry, _, err := defaultRepos()
	if err != nil {
		return nil, err
	}
	layout, err := resolveProject()
	if err != nil {
		return nil, err
	}
	layers, watched, err := servedConfigLayers(sddDir)
	if err != nil {
		return nil, err
	}
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
		return nil, err
	}
	stableRepoRoot, err := git.StableRepoRoot(filepath.Dir(sddDir))
	if err != nil {
		return nil, err
	}
	storeLocations, err := resolveSessionLocations(sddDir, cfg, locations)
	if err != nil {
		return nil, err
	}
	project := sessionStoreProject(cfg)
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: project, GraphDir: graphDir, MetaPath: filepath.Join(sddDir, model.SchemaMetaFileName), Contribution: cfg.Contribution})
	if err != nil {
		return nil, err
	}
	sessions, err := localadapter.NewFilesystemSessionStore(storeLocations...)
	if err != nil {
		return nil, err
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStore(storeLocations...)
	if err != nil {
		return nil, err
	}
	localEmbedder, err := buildEmbedder(cmd)
	if err != nil {
		return nil, err
	}
	crossEmbedder, err := crossRepoEmbedder(cmd)
	if err != nil {
		return nil, err
	}
	// The machine-global persistent store the CLI also builds and reads: one
	// store per (repo-key, embedding fingerprint) under the cache root. The
	// base repo key matches the CLI exactly (see cmd search resolveIndexStore);
	// the fingerprint selects the final StoreDir inside the adapter at request
	// time, so a lazy embedder that reveals its dimensionality only on first
	// use still routes correctly. No process-local memory store in production.
	cacheRoot := registry.CacheRoot()
	baseRepoKey := persistentIndexRepoKey(cfg, stableRepoRoot)
	baseIndex := localadapter.NewPersistentSearchIndexStore(project, cacheRoot, baseRepoKey).WithCheckout(graphDir)
	var embeddings embed.Embedder
	var runeCap func(context.Context) (int, error)
	if localEmbedder.Embedder != nil {
		embeddings = localEmbedder
		runeCap = localEmbedder.MaxRunes
	}
	serverCheckout := filepath.Dir(sddDir)
	localLayer, err := localConfigLayer()
	if err != nil {
		return nil, err
	}
	targets, err := localadapter.NewRepositoryTargets(ctx, project, serverCheckout, locations.ConfigPath, localLayer, locations.StateRoot)
	if err != nil {
		return nil, err
	}
	if cfg == nil || cfg.DefaultBranch == "" {
		return nil, fmt.Errorf("default_branch is required in .sdd/config.yaml before serving mutation tools")
	}
	_, manager, err := defaultRepos()
	if err != nil {
		return nil, err
	}
	round, err := cloneRound()
	if err != nil {
		return nil, err
	}
	closure := finders.New(finders.Options{Config: cfg, Repos: registry})
	rounds := connectedRounds{handler: handlers.New(handlers.Options{Reader: closure, Repos: manager, CloneRound: round})}
	prefilter, err := projectPrefilter(cfg)
	if err != nil {
		return nil, err
	}
	verdicts, err := projectRelevanceStore(sddDir, locations.StateRoot, baseRepoKey, cfg)
	if err != nil {
		return nil, err
	}
	events, err := projectEventStore(sddDir, graphDir, cfg)
	if err != nil {
		return nil, err
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: project, DisplayName: displayName}, DefaultBranch: cfg.DefaultBranch, Language: language,
		Dependencies: dependencies, Graph: localadapter.BranchReadStore{GraphStore: graph, Branches: targets, DefaultBranch: cfg.DefaultBranch}, Targets: targets, Sync: targets, Branches: targets, Base: targets,
		Connected: rounds,
		Embedder:  embeddings, SearchIndex: optionalSearchIndex(embeddings, baseIndex), RuneCap: runeCap,
		LLM:       runner,
		Grounding: groundingPolicy(cfg),
		Verifier:  verifierPolicy(cfg), Window: modelWindow(cfg.LLM), Checkout: os.DirFS(layout.WorkRoot),
		Hooks:     hooks.New(hooks.Sources{GlobalConfig: locations.ConfigPath, Project: layout, LocalConfig: localConfigOverride}, hooks.NewStore(locations.StateRoot)),
		Events:    events,
		HandEdits: localadapter.GitHandEdits{GraphDir: graphDir},
		Relevance: verdicts,
		Prefilter: prefilter,
	})
	if err != nil {
		return nil, err
	}
	dialogue, err := localDialogue(sddDir)
	if err != nil {
		return nil, err
	}
	var memberEmbedder embed.Embedder
	var memberRuneCap func(context.Context) (int, error)
	if crossEmbedder.Embedder != nil {
		memberEmbedder = crossEmbedder
		memberRuneCap = crossEmbedder.MaxRunes
	}
	access := &localRuntimeAccess{
		project: project, participant: participant, dialogue: dialogue, runtime: runtime,
		dependencies: &connectedDependencies{registry: registry, manager: manager, closure: closure, rounds: rounds,
			writer: sdd.RepoID(cfg.RepoID), connected: cfg.Connected,
			build: func(repoID sdd.RepoID, dir string, dependencyCfg *model.PerRepoConfig) (*sdd.ProjectRuntime, error) {
				return cloneRuntime(ctx, repoID, dir, dependencyCfg, locations, runner, modelWindow(cfg.LLM), memberEmbedder, memberRuneCap, cacheRoot)
			}},
	}
	return &servedProject{
		access: access, cfg: cfg, layout: layout, sessions: sessions, blobs: blobs,
		graphDir: graphDir, entries: graphFolderEntries(graphDir).Entries, layers: layers, watched: watched,
	}, nil
}

// cloneRuntime serves a dependency from its sdd-owned clone at dir: its own
// config, default branch, graph and write targets. Connected stores are keyed
// by the dependency's repo ID and exclude embedded entries, so binary-shipped
// base facts embed once per machine in the base store.
func cloneRuntime(ctx context.Context, repoID sdd.RepoID, dir string, cfg *model.PerRepoConfig, locations repos.Locations, runner pkgllm.Runner, window func(context.Context) (sdd.ModelWindow, error), embeddings embed.Embedder, runeCap func(context.Context) (int, error), cacheRoot string) (*sdd.ProjectRuntime, error) {
	project := sdd.ProjectID(repoID)
	defaultBranch := cfg.DefaultBranch
	if defaultBranch == "" {
		branch, err := git.CurrentBranch(dir)
		if err != nil {
			return nil, err
		}
		defaultBranch = branch
	}
	targets, err := localadapter.NewRepositoryTargets(ctx, project, dir, locations.ConfigPath, "", locations.StateRoot)
	if err != nil {
		return nil, err
	}
	cloneGraphDir := meta.ResolveGraphDir(dir, cfg)
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: project, GraphDir: cloneGraphDir, MetaPath: filepath.Join(dir, model.SDDDirName, model.SchemaMetaFileName)})
	if err != nil {
		return nil, err
	}
	searchIndex := localadapter.NewPersistentSearchIndexStore(project, cacheRoot, index.RepoKey(repoID, dir)).WithCheckout(meta.ResolveGraphDir(dir, cfg))
	verdicts, err := projectRelevanceStore(filepath.Join(dir, model.SDDDirName), locations.StateRoot, index.RepoKey(repoID, dir), cfg)
	if err != nil {
		return nil, err
	}
	events, err := projectEventStore(filepath.Join(dir, model.SDDDirName), cloneGraphDir, cfg)
	if err != nil {
		return nil, err
	}
	return sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: project, DisplayName: string(repoID)}, DefaultBranch: defaultBranch, Language: cfg.Language,
		Dependencies: cfg.Dependencies, Graph: localadapter.BranchReadStore{GraphStore: graph, Branches: targets, DefaultBranch: defaultBranch},
		Targets: targets, Sync: targets, Branches: targets, Base: targets,
		Embedder: embeddings, SearchIndex: optionalSearchIndex(embeddings, searchIndex), RuneCap: runeCap, LLM: runner,
		ExcludeEmbeddedFromIndex: true,
		Grounding:                groundingPolicy(cfg),
		Verifier:                 verifierPolicy(cfg),
		Window:                   window,
		Relevance:                verdicts,
		Events:                   events,
		HandEdits:                localadapter.GitHandEdits{GraphDir: cloneGraphDir},
	})
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

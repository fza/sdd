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
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urfave/cli/v3"

	sdd "github.com/networkteam/sdd/pkg/application"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/hooks"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/secrets"
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
			&cli.BoolFlag{
				Name:  "output-schema",
				Usage: "For debugging and for programs that read the replies as data, not through a model: lists each tool's reply schema. Agents don't need it.",
			},
			&cli.IntFlag{
				Name:    "mcp-output-tokens",
				Usage:   "Claude Code's MCP output limit in tokens: lowers the size of a result served to Claude Code to 2 characters per token",
				Sources: cli.EnvVars("MAX_MCP_OUTPUT_TOKENS"),
				Validator: func(tokens int) error {
					if tokens < 0 {
						return fmt.Errorf("MCP output limit %d is negative", tokens)
					}
					return nil
				},
			},
		),
		Action: withWriteGate(func(ctx context.Context, cmd *cli.Command) error {
			watched, err := servedConfigFiles()
			if err != nil {
				return err
			}
			watch, err := newConfigWatch(watched, func(previous *servedProject, reads []configRead) (*servedProject, error) {
				return buildServedProject(ctx, cmd, "", previous, reads)
			})
			if err != nil {
				return err
			}
			transport := cmd.String("transport")
			// Only a stdio client shares this filesystem; an HTTP client must
			// never stage a file of the server's by naming its path.
			var readLocalFile func(string) ([]byte, error)
			if transport == "stdio" {
				readLocalFile = os.ReadFile
			}
			application, identity, err := buildServedApplication(ctx, cmd, watch, readLocalFile)
			if err != nil {
				return err
			}
			var cacheNotice func(context.Context) string
			if launchCachePrune != nil {
				timerCtx, stopTimer := context.WithCancel(ctx)
				defer stopTimer()
				go runCachePruneTimer(timerCtx, launchCachePrune)
				cacheNotice = serveCacheNotice
			}

			srv, err := mcpserver.New(mcpserver.Options{SearchSyncMode: sdd.SearchSyncAll,
				Application:     application,
				LocalIdentity:   identity,
				LocalClient:     transport == "stdio",
				Version:         cmd.Root().Version,
				OutputSchema:    cmd.Bool("output-schema"),
				MCPOutputTokens: cmd.Int("mcp-output-tokens"),
				BeforeCall: func(ctx context.Context, session string) (context.Context, mcpserver.Notices, error) {
					call, notice, err := watch.admit(ctx, session)
					notices := mcpserver.Notices{Config: notice}
					if cacheNotice != nil {
						notices.Cache = cacheNotice(ctx)
					}
					return call, notices, err
				},
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

// localApplication builds the application over the project the working
// directory belongs to, from one read of its config files, its LLM runner
// bounded by the duration flag timeoutFlag names.
func localApplication(ctx context.Context, cmd *cli.Command, timeoutFlag string) (*sdd.Application, sdd.ProjectID, sdd.RequestIdentity, error) {
	watched, err := servedConfigFiles()
	if err != nil {
		return nil, "", sdd.RequestIdentity{}, err
	}
	served, err := buildServedProject(ctx, cmd, timeoutFlag, nil, readConfigFiles(watched))
	if err != nil {
		return nil, "", sdd.RequestIdentity{}, err
	}
	application, identity, err := newLocalApplication(ctx, cmd, served, sdd.ApplicationOptions{Access: served.access, Sessions: served.sessions, StagedBlobs: served.blobs})
	if err != nil {
		return nil, "", sdd.RequestIdentity{}, err
	}
	return application, served.access.project, identity, nil
}

// localDialogue reads dialogue.mode and the layer that sets it; the
// committed layer arrives without it.
func localDialogue(sddDir string) (sdd.Dialogue, error) {
	loc, err := repos.DefaultLocations()
	if err != nil {
		return sdd.Dialogue{}, err
	}
	global, err := repos.LoadConfigFrom(loc.ConfigPath)
	if err != nil {
		return sdd.Dialogue{}, err
	}
	committed, local, err := meta.ReadConfigLayers(sddDir)
	if err != nil {
		return sdd.Dialogue{}, err
	}
	sources := map[model.ConfigValueSource]string{
		model.ConfigSourceGlobal: loc.ConfigPath,
		model.ConfigSourceLocal:  filepath.Join(sddDir, "config.local.yaml"),
	}
	for _, value := range model.EffectiveConfigValues(global.BaseConfig, committed, local) {
		if value.Key == "dialogue.mode" {
			return sdd.Dialogue{Mode: value.Value, Source: sources[value.Source]}, nil
		}
	}
	return sdd.Dialogue{Mode: string(model.DialogueGuided)}, nil
}

// The positions of the config files in what servedConfigFiles returns.
const (
	globalConfigFile = iota
	committedConfigFile
	localConfigFile
	servedConfigCount
)

// servedConfigFiles names the config files serve resolves its config from,
// existing or not.
func servedConfigFiles() ([]string, error) {
	loc, err := repos.DefaultLocations()
	if err != nil {
		return nil, err
	}
	sddDir, err := resolveSDDDir()
	if err != nil {
		return nil, err
	}
	paths := make([]string, servedConfigCount)
	paths[globalConfigFile] = loc.ConfigPath
	paths[committedConfigFile] = filepath.Join(sddDir, "config.yaml")
	paths[localConfigFile] = filepath.Join(sddDir, "config.local.yaml")
	return paths, nil
}

// servedConfigLayers parses the reads of the files servedConfigFiles names.
func servedConfigLayers(reads []configRead) (model.ConfigLayers, error) {
	var (
		layers model.ConfigLayers
		err    error
	)
	global := reads[globalConfigFile]
	if global.err != nil {
		return model.ConfigLayers{}, fmt.Errorf("reading global config %s: %w", global.path, global.err)
	}
	if global.found {
		cfg, err := repos.ParseConfigFile(global.path, global.data)
		if err != nil {
			return model.ConfigLayers{}, err
		}
		layers.Global = cfg.BaseConfig
	}
	layers.Project, err = perRepoLayer(reads[committedConfigFile])
	if err != nil {
		return model.ConfigLayers{}, err
	}
	layers.Local, err = perRepoLayer(reads[localConfigFile])
	if err != nil {
		return model.ConfigLayers{}, err
	}
	return layers, nil
}

func perRepoLayer(read configRead) (*model.PerRepoConfig, error) {
	if read.err != nil {
		return nil, read.err
	}
	if !read.found {
		return nil, nil
	}
	return meta.ParseConfigFile(read.path, read.data)
}

// buildServedApplication composes the application whose every call resolves
// against the project watch serves at that moment.
func buildServedApplication(ctx context.Context, cmd *cli.Command, watch *configWatch, readLocalFile func(string) ([]byte, error)) (*sdd.Application, sdd.RequestIdentity, error) {
	return newLocalApplication(ctx, cmd, watch.served(), sdd.ApplicationOptions{Access: servedAccess{watch}, Sessions: servedSessions{watch}, StagedBlobs: servedBlobs{watch}, ReadLocalFile: readLocalFile})
}

// newLocalApplication completes options with the headless delegate launch,
// which reads served's config, and the round processes, and checks the
// application answers for served's project.
func newLocalApplication(ctx context.Context, cmd *cli.Command, served *servedProject, options sdd.ApplicationOptions) (*sdd.Application, sdd.RequestIdentity, error) {
	sddDir, err := resolveSDDDir()
	if err != nil {
		return nil, sdd.RequestIdentity{}, err
	}
	rounds, err := newRoundProcesses(cmd)
	if err != nil {
		return nil, sdd.RequestIdentity{}, err
	}
	project := served.access.project
	options.DelegateLauncher = headlessLauncher{project: project, workRoot: filepath.Dir(sddDir), cfg: served.cfg}
	options.RoundProcesses = rounds
	options.SecretRules = secretRulesReport
	application, err := sdd.NewApplication(options)
	if err != nil {
		return nil, sdd.RequestIdentity{}, err
	}
	identity := sdd.RequestIdentity{Subject: "local"}
	_, err = application.Info(ctx, identity, project, sdd.InfoRequest{})
	if err != nil {
		return nil, sdd.RequestIdentity{}, err
	}
	return application, identity, nil
}

// buildServedProject resolves the project serve answers calls with from one
// read of its config files, its LLM client included, bounded by the duration
// flag timeoutFlag names. ctx is the server's.
func buildServedProject(ctx context.Context, cmd *cli.Command, timeoutFlag string, previous *servedProject, reads []configRead) (*servedProject, error) {
	sddDir, err := resolveSDDDir()
	if err != nil {
		return nil, err
	}
	layers, err := servedConfigLayers(reads)
	if err != nil {
		return nil, err
	}
	global, committed, local, _ := model.HonoredVerifyLayers(layers.Global, layers.Project, layers.Local, meta.ConfigLayerFiles(sddDir))
	cfg := model.ConfigLayers{Global: global, Project: committed, Local: local}.Effective()
	graphDir, ok := graphDirFlag(cmd)
	if !ok {
		graphDir = meta.ResolveGraphDir(filepath.Dir(sddDir), cfg)
	}
	registry, _, err := defaultRepos()
	if err != nil {
		return nil, err
	}
	clients, err := buildServedClients(cmd, timeoutFlag, previous, layers)
	if err != nil {
		return nil, err
	}
	runner, localEmbedder, crossEmbedder := clients.llm.Runner, clients.embedders.local, clients.embedders.cross
	displayName := filepath.Base(filepath.Dir(sddDir))
	participant := ""
	language := ""
	contribution := ""
	var dependencies []sdd.RepoID
	if cfg != nil {
		participant = cfg.Participant
		language = cfg.Language
		contribution = cfg.Contribution
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
	storeLocations, err := resolveSessionLocations(ctx, sddDir, cfg, locations)
	if err != nil {
		return nil, err
	}
	// A server that writes no session for a while still uses its project.
	if err := localadapter.MarkProjectUsed(locations.StateRoot, persistentIndexRepoKey(cfg, stableRepoRoot), time.Now()); err != nil {
		return nil, err
	}
	project := sessionStoreProject(cfg)
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: project, GraphDir: graphDir, Contribution: contribution})
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
	targets, err := localadapter.NewRepositoryTargets(project, filepath.Dir(sddDir), locations.ConfigPath)
	if err != nil {
		return nil, err
	}
	if cfg == nil || cfg.DefaultBranch == "" {
		return nil, fmt.Errorf("default_branch is required in .sdd/config.yaml before serving mutation tools")
	}
	events, err := projectEventStore(sddDir, graphDir, cfg)
	if err != nil {
		return nil, err
	}
	verdicts, err := projectRelevanceStore(locations.StateRoot, baseRepoKey)
	if err != nil {
		return nil, err
	}
	prefilter, err := projectPrefilter(cfg)
	if err != nil {
		return nil, err
	}
	layout, err := resolveProject()
	if err != nil {
		return nil, err
	}
	workRoot := filepath.Dir(sddDir)
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: project, DisplayName: displayName}, DefaultBranch: cfg.DefaultBranch, Language: language,
		Dependencies: dependencies, Graph: localadapter.BranchReadStore{GraphStore: graph, Branches: targets, DefaultBranch: cfg.DefaultBranch}, Targets: targets, Branches: targets, Base: targets,
		Embedder: embeddings, SearchIndex: optionalSearchIndex(embeddings, baseIndex), RuneCap: runeCap,
		LLM: runner, Events: events, Relevance: verdicts, Prefilter: prefilter,
		HandEdits: localadapter.GitHandEdits{GraphDir: graphDir},
		Grounding: groundingPolicy(cfg), Verifier: verifierPolicy(cfg), ContextTokens: cfg.LLM.ContextTokens,
		Checkout: os.DirFS(workRoot), Commits: git.CLI{Dir: workRoot}, Git: localadapter.GitHistory{Dir: workRoot},
		Hooks: hooks.New(hooks.Sources{GlobalConfig: locations.ConfigPath, Project: layout, Tracker: git.TrackedFiles{}}, hooks.NewAllows(locations.StateRoot)),
	})
	if err != nil {
		return nil, err
	}
	var memberEmbedder embed.Embedder
	var memberRuneCap func(context.Context) (int, error)
	if crossEmbedder.Embedder != nil {
		memberEmbedder = crossEmbedder
		memberRuneCap = crossEmbedder.MaxRunes
	}
	dialogue, err := localDialogue(sddDir)
	if err != nil {
		return nil, err
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
			memberIndex := localadapter.NewPersistentSearchIndexStore(sdd.ProjectID(cache.RepoID), cacheRoot, index.RepoKey(cache.RepoID, cache.Dir)).WithCheckout(graphDir)
			// A connected repository's checkout and commits back the files and
			// commits a deep verifier cites there.
			return sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
				Project: sdd.ProjectRef{ID: sdd.ProjectID(cache.RepoID), DisplayName: string(cache.RepoID)}, Graph: memberGraph,
				Language: dependencyCfg.Language, Dependencies: dependencyCfg.Dependencies,
				Embedder: memberEmbedder, SearchIndex: optionalSearchIndex(memberEmbedder, memberIndex), RuneCap: memberRuneCap, LLM: runner,
				ExcludeEmbeddedFromIndex: true,
				Verifier:                 verifierPolicy(dependencyCfg),
				Checkout:                 os.DirFS(cache.Dir), Commits: git.CLI{Dir: cache.Dir}, Git: localadapter.GitHistory{Dir: cache.Dir},
			})
		}},
	}
	return &servedProject{access: access, cfg: cfg, sessions: sessions, blobs: blobs, layers: layers, clients: clients}, nil
}

// servedClients are the LLM runner and the embedders a served project calls.
// A rebuild keeps each group whose keys did not change, and with it the rate
// limit its calls are counted against.
type servedClients struct {
	llm       *servedRunner
	embedders *servedEmbedders
}

type servedRunner struct{ pkgllm.Runner }

type servedEmbedders struct{ local, cross handlers.IndexEmbedder }

func buildServedClients(cmd *cli.Command, timeoutFlag string, previous *servedProject, layers model.ConfigLayers) (*servedClients, error) {
	clients := &servedClients{}
	if previous != nil {
		changes := model.ConfigChanges(previous.layers, layers)
		if !changedUnder(changes, "llm.") {
			clients.llm = previous.clients.llm
		}
		// The cross-repo embedder follows the user-global embedding alone,
		// which a project layer can hide from the effective keys.
		if !changedUnder(changes, "embedding.") && reflect.DeepEqual(previous.layers.Global.Embedding, layers.Global.Embedding) {
			clients.embedders = previous.clients.embedders
		}
	}
	cfg := layers.Effective()
	if clients.llm == nil {
		runner, err := composeRunner(llmConfigWithFlags(cmd, cfg), timeoutOptions(cmd, timeoutFlag)...)
		if err != nil {
			return nil, err
		}
		clients.llm = &servedRunner{runner}
	}
	if clients.embedders == nil {
		embedding := embeddingConfigWithFlags(cmd, cfg)
		local, err := configuredEmbedder(embedding)
		if err != nil {
			return nil, err
		}
		// As crossRepoEmbedder: the user-global embedding wins when set.
		if layers.Global.Embedding.Provider != "" {
			embedding = layers.Global.Embedding
		}
		cross, err := configuredEmbedder(embedding)
		if err != nil {
			return nil, err
		}
		clients.embedders = &servedEmbedders{local: local, cross: cross}
	}
	return clients, nil
}

func changedUnder(changes []model.ConfigChange, prefix string) bool {
	return slices.ContainsFunc(changes, func(change model.ConfigChange) bool { return strings.HasPrefix(change.Key, prefix) })
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

// secretRulesReport is the application's SecretRules: the rules
// useSecretRules selected for this process's scans.
func secretRulesReport() (sdd.SecretRules, error) {
	rules, err := secrets.Active().Status()
	if err != nil {
		return sdd.SecretRules{}, err
	}
	return sdd.SecretRules{
		Source: rules.Source, Tag: rules.Tag, Commit: rules.Commit, Update: string(rules.Update), Unused: rules.Unused,
		LastAccepted: rules.LastAccepted, LastChecked: rules.LastChecked, LastFailure: rules.LastFailure, LastFailureAt: rules.LastFailureAt,
	}, nil
}

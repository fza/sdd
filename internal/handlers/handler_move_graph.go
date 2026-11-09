package handlers

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/index"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
)

// GraphMover is the git work a graph move does in one repository.
type GraphMover interface {
	UpstreamLocator
	RequireSubtree(ctx context.Context) error
	Head(ctx context.Context) (string, error)
	Uncommitted(ctx context.Context, paths []string) ([]string, error)
	SplitSubtree(ctx context.Context, prefix, revision string) (string, error)
	FetchCommitFrom(ctx context.Context, sourceDir, commit string) error
	BuildCommit(ctx context.Context, plan model.MoveCommit) (string, error)
	Land(ctx context.Context, commit string) error
	RecordCloneCommitsSince(ctx context.Context, since string) error
	UpstreamRef(ctx context.Context) (string, error)
	Fetch(ctx context.Context) error
	MergePull(ctx context.Context) (string, error)
	ChangedSince(ctx context.Context, revision, ref, prefix string) (bool, error)
	BackfillHistory(ctx context.Context) error
	FileHistory(ctx context.Context, prefix string, paths []string) ([]byte, error)
	ApplyHistory(ctx context.Context, series []byte, directory string) error
	CommitAllowEmpty(ctx context.Context, message string, paths ...string) error
	MoveTracked(ctx context.Context, from, to string) error
}

// StrayFinder lists the entries the former locations of a graph hold.
type StrayFinder interface {
	GraphStrays(ctx context.Context, q query.GraphStraysQuery) (*query.GraphStraysResult, error)
}

// moveAttempts bounds how often a move splits the history again because
// the source graph moved on upstream while it ran.
const moveAttempts = 3

// graphHome is where a graph lives: its location, the repository holding it
// and the folder in that repository.
type graphHome struct {
	location model.LocationRef
	root     string
	graphDir string
	clone    model.RepoID
}

func (g graphHome) same(other graphHome) bool {
	return filepath.Clean(g.root) == filepath.Clean(other.root) && path(g.graphDir) == path(other.graphDir)
}

func path(dir string) string { return strings.Trim(filepath.ToSlash(filepath.Clean(dir)), "/") }

// moveRepo collects what a move changes in one repository.
type moveRepo struct {
	root   string
	mover  GraphMover
	clone  model.RepoID
	head   string
	plan   model.MoveCommit
	commit string
}

func (r *moveRepo) write(path string, content []byte) {
	r.plan.Files = append(r.plan.Files, model.MoveFile{Path: path, Content: content})
}

func (r *moveRepo) remove(path string) {
	r.plan.Files = append(r.plan.Files, model.MoveFile{Path: path, Remove: true})
}

func (r *moveRepo) touched() []string {
	paths := slices.Clone(r.plan.Remove)
	for _, file := range r.plan.Files {
		paths = append(paths, file.Path)
	}
	return paths
}

// MoveGraph moves the graph of cmd.WorkRoot's layout to cmd.Target with its
// history and leaves a move record in every repository it changes. With the
// graph at the target already it carries over the entries its former
// locations hold instead.
func (h *Handler) MoveGraph(ctx context.Context, cmd *command.MoveGraphCmd) error {
	if h.moverAt == nil || h.projectRepos == nil {
		return errors.New("moving the graph needs a git adapter")
	}
	project, err := meta.ResolveProject(cmd.WorkRoot, h.cloneDir())
	if err != nil {
		return err
	}
	source, err := h.sourceHome(project)
	if err != nil {
		return err
	}
	target, err := h.targetHome(ctx, cmd)
	if err != nil {
		return err
	}
	if source.same(target) {
		return h.carryStrays(ctx, cmd, project, source)
	}
	for attempt := 1; ; attempt++ {
		movedOn, err := h.moveGraph(ctx, cmd, project, source, target)
		if err != nil || !movedOn {
			return err
		}
		if attempt == moveAttempts {
			return fmt.Errorf("the graph at %s kept moving on upstream while the move split its history; nothing was changed, run the move again", source.location)
		}
	}
}

func (h *Handler) cloneDir() func(model.RepoID) (string, error) {
	if h.graphClones == nil {
		return nil
	}
	return h.graphClones.Dir
}

func (h *Handler) sourceHome(project meta.Project) (graphHome, error) {
	committed, _, err := meta.ReadConfigLayers(project.SDDDir(), "")
	if err != nil {
		return graphHome{}, err
	}
	if committed == nil {
		return graphHome{}, fmt.Errorf("%s: %w", project.SDDDir(), meta.ErrNotAProject)
	}
	graphDir := committed.GraphDir
	if graphDir == "" {
		graphDir = model.DefaultGraphDir
	}
	if filepath.IsAbs(graphDir) || !filepath.IsLocal(graphDir) {
		return graphHome{}, fmt.Errorf("graph_dir %s lies outside its repository; sdd move moves a graph inside one", graphDir)
	}
	home := graphHome{root: project.Root, graphDir: path(graphDir)}
	switch {
	case project.GraphRepo != "":
		home.clone = project.GraphRepo
		home.location = model.GitRepoLocation(project.GraphRepo, home.graphDir, committed.DefaultBranch)
	case project.Pointed():
		pointer, err := meta.ReadConfig(meta.SDDDir(project.WorkRoot), "")
		if err != nil {
			return graphHome{}, err
		}
		home.location = model.GitPathLocation(pointer.ProjectDir, home.graphDir)
	default:
		home.location = model.GitDirLocation(home.graphDir)
	}
	return home, nil
}

func (h *Handler) targetHome(ctx context.Context, cmd *command.MoveGraphCmd) (graphHome, error) {
	target := cmd.Target
	graphDir := target.Dir()
	if graphDir == "" {
		graphDir = model.GraphOnlyGraphDir
	}
	switch target.Kind {
	case model.LocationGitDir:
		graphDir = target.Address
	case model.LocationGitPath, model.LocationGitRepo:
	default:
		return graphHome{}, fmt.Errorf("sdd move cannot move a graph to a %s location", target.Kind)
	}
	if filepath.IsAbs(graphDir) || !filepath.IsLocal(graphDir) {
		return graphHome{}, fmt.Errorf("graph folder %s lies outside its repository; name a folder inside it", graphDir)
	}
	err := gitignoreLine(graphDir)
	if err != nil {
		return graphHome{}, fmt.Errorf("graph folder: %w", err)
	}
	home := graphHome{graphDir: path(graphDir)}
	switch target.Kind {
	case model.LocationGitDir:
		home.root = cmd.WorkRoot
		home.location = model.GitDirLocation(home.graphDir)
	case model.LocationGitPath:
		root := target.Address
		if !filepath.IsAbs(root) {
			root = filepath.Join(cmd.WorkRoot, root)
		}
		home.root = filepath.Clean(root)
		if within(home.root, cmd.WorkRoot) || within(cmd.WorkRoot, home.root) {
			return graphHome{}, fmt.Errorf("--project-dir %s overlaps this repository; the graph needs a repository of its own beside it", target.Address)
		}
		repoRoot, err := h.projectRepos.RootOf(home.root)
		if err != nil {
			return graphHome{}, err
		}
		if repoRoot != "" && !sameDir(repoRoot, home.root) {
			return graphHome{}, fmt.Errorf("--project-dir %s lies inside the repository at %s; the graph needs a repository of its own", target.Address, repoRoot)
		}
		home.location = model.GitPathLocation(target.Address, home.graphDir)
	case model.LocationGitRepo:
		repo := model.RepoID(target.Address)
		if h.graphClones == nil {
			return graphHome{}, errors.New("graph_repo needs sdd's graph clones")
		}
		err := h.EnsureGraphClone(ctx, &command.EnsureGraphCloneCmd{RepoID: repo, CodeOrigin: h.projectRepos.RemoteURL(cmd.WorkRoot)})
		if err != nil {
			return graphHome{}, err
		}
		home.root, err = h.graphClones.Dir(repo)
		if err != nil {
			return graphHome{}, err
		}
		branch := target.Branch()
		if branch == "" {
			branch, err = h.projectRepos.CurrentBranch(home.root)
			if err != nil {
				return graphHome{}, err
			}
		}
		home.clone = repo
		home.location = model.GitRepoLocation(repo, home.graphDir, branch)
	}
	return home, nil
}

// moveGraph makes one attempt at the move. movedOn reports that the source
// graph moved on upstream since the split, in which case nothing changed and
// the source was brought up to date for the next attempt.
func (h *Handler) moveGraph(ctx context.Context, cmd *command.MoveGraphCmd, project meta.Project, source, target graphHome) (movedOn bool, err error) {
	same := filepath.Clean(source.root) == filepath.Clean(target.root)
	repos := map[string]*moveRepo{}
	var order []*moveRepo
	repoAt := func(root string, clone model.RepoID) (*moveRepo, error) {
		key := filepath.Clean(root)
		if repo := repos[key]; repo != nil {
			return repo, nil
		}
		repo := &moveRepo{root: root, mover: h.moverAt(root), clone: clone}
		if _, statErr := os.Stat(root); statErr == nil {
			head, err := repo.mover.Head(ctx)
			if err != nil {
				return nil, err
			}
			repo.head = head
		}
		repos[key] = repo
		order = append(order, repo)
		return repo, nil
	}
	s, err := repoAt(source.root, source.clone)
	if err != nil {
		return false, err
	}
	revision := s.head
	if revision == "" {
		return false, fmt.Errorf("%s holds no commit yet; commit the graph before moving it", source.root)
	}
	dirty, err := s.mover.Uncommitted(ctx, []string{source.graphDir})
	if err != nil {
		return false, err
	}
	if len(dirty) > 0 {
		return false, fmt.Errorf("the graph folder %s holds uncommitted changes; commit or discard them before moving the graph", filepath.Join(source.root, source.graphDir))
	}
	if !same {
		err = s.mover.RequireSubtree(ctx)
		if err != nil {
			return false, err
		}
	}
	vacated := filepath.Clean(source.root) != filepath.Clean(cmd.WorkRoot) && !same
	var vacate []model.MoveFile
	if vacated {
		vacate, err = h.vacatePlan(ctx, source.root)
		if err != nil {
			return false, err
		}
	}
	err = h.prepareTarget(target, source, cmd)
	if err != nil {
		return false, err
	}
	t, err := repoAt(target.root, target.clone)
	if err != nil {
		return false, err
	}
	w, err := repoAt(cmd.WorkRoot, "")
	if err != nil {
		return false, err
	}
	held := len(entryFilesIn(filepath.Join(source.root, source.graphDir)))
	id, err := moveID(h.now())
	if err != nil {
		return false, err
	}
	record := model.MoveRecord{ID: id, From: source.location, To: target.location, Revision: revision, Entries: held}
	message := fmt.Sprintf("sdd: move graph from %s to %s\n\n%s", source.location, target.location, record.Trailers())
	for _, repo := range order {
		repo.plan.Message = message
		repo.plan.Parents = []string{repo.head}
	}

	if same {
		s.plan.Remove = append(s.plan.Remove, source.graphDir)
		s.plan.Graft, s.plan.GraftAt = revision+":"+source.graphDir, target.graphDir
	} else {
		if source.clone != "" {
			err = h.gated(ctx, gitgate.Fetch, s.mover, s.mover.BackfillHistory)
			if err != nil {
				return false, fmt.Errorf("fetching the history of %s before splitting it: %w", source.clone, err)
			}
		}
		split, err := s.mover.SplitSubtree(ctx, source.graphDir, revision)
		if err != nil {
			return false, err
		}
		err = t.mover.FetchCommitFrom(ctx, source.root, split)
		if err != nil {
			return false, err
		}
		s.plan.Remove = append(s.plan.Remove, source.graphDir)
		t.plan.Graft, t.plan.GraftAt = split, target.graphDir
		t.plan.Parents = append(t.plan.Parents, split)
	}

	err = h.planMeta(cmd, s, t, same)
	if err != nil {
		return false, err
	}
	err = h.planConfigs(ctx, target, s, t, w, same)
	if err != nil {
		return false, err
	}
	err = h.planRepoFiles(source, target, s, t, w)
	if err != nil {
		return false, err
	}
	s.plan.Files = append(s.plan.Files, vacate...)
	if target.location.Kind == model.LocationGitPath && t != w {
		install, err := h.workInstallPlan(ctx, w.root, t.root, cmd.BinaryVersion)
		if err != nil {
			return false, err
		}
		t.plan.Files = append(t.plan.Files, install...)
	}

	for _, repo := range order {
		if len(repo.plan.Files) == 0 && len(repo.plan.Remove) == 0 && repo.plan.Graft == "" {
			continue
		}
		dirty, err := repo.mover.Uncommitted(ctx, repo.touched())
		if err != nil {
			return false, err
		}
		dirty = slices.DeleteFunc(dirty, func(p string) bool { return repo == s && p == source.graphDir })
		if len(dirty) > 0 {
			return false, fmt.Errorf("%s holds uncommitted changes in %s, which the move rewrites; commit or discard them first", repo.root, strings.Join(dirty, ", "))
		}
		repo.commit, err = repo.mover.BuildCommit(ctx, repo.plan)
		if err != nil {
			return false, err
		}
	}

	movedOn, err = h.sourceMovedOn(ctx, s, revision, source)
	if err != nil || movedOn {
		return movedOn, err
	}
	oldRepo, err := committedRepoID(meta.SDDDir(source.root))
	if err != nil {
		return false, err
	}

	for _, repo := range []*moveRepo{t, s, w} {
		if repo.commit == "" {
			continue
		}
		err = repo.mover.Land(ctx, repo.commit)
		if err != nil {
			return false, fmt.Errorf("landing the move in %s: %w", repo.root, err)
		}
		err = repo.mover.RecordCloneCommitsSince(ctx, repo.head)
		if err != nil {
			return false, err
		}
		repo.commit = ""
	}
	for _, repo := range []*moveRepo{t, s} {
		if repo.clone == "" {
			continue
		}
		err = h.graphClones.Publish(ctx, repo.clone)
		if err != nil {
			return false, err
		}
	}
	if target.clone != "" {
		err = h.EnsureGraphClone(ctx, &command.EnsureGraphCloneCmd{RepoID: target.clone, CodeOrigin: h.projectRepos.RemoteURL(cmd.WorkRoot)})
		if err != nil {
			return false, err
		}
	}
	err = h.moveMachineLocal(cmd, project, source, target, oldRepo)
	if err != nil {
		return false, err
	}
	if vacated {
		// The machine-local folders moved out leave .sdd/ empty, which git
		// keeps no record of.
		pruneEmptyDirs(meta.SDDDir(source.root))
	}
	if cmd.OnMoved != nil {
		cmd.OnMoved(record)
	}
	return false, nil
}

// prepareTarget refuses a target already holding a graph, and creates and
// initializes the checkout of a git-path target that does not exist yet.
func (h *Handler) prepareTarget(target, source graphHome, cmd *command.MoveGraphCmd) error {
	if len(entryFilesIn(filepath.Join(target.root, target.graphDir))) > 0 {
		return fmt.Errorf("%s holds graph entries already; sdd move moves a graph only to a place holding none", filepath.Join(target.root, target.graphDir))
	}
	if filepath.Clean(target.root) == filepath.Clean(cmd.WorkRoot) || filepath.Clean(target.root) == filepath.Clean(source.root) {
		return nil
	}
	configPath := filepath.Join(target.root, model.SDDDirName, "config.yaml")
	_, err := os.Stat(configPath)
	if err == nil {
		return fmt.Errorf("%s holds an sdd project already; sdd move moves a graph only to a repository holding none", target.root)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if target.location.Kind != model.LocationGitPath {
		return nil
	}
	repoRoot, err := h.projectRepos.RootOf(target.root)
	if err != nil || repoRoot != "" {
		return err
	}
	err = os.MkdirAll(target.root, 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", target.root, err)
	}
	return h.projectRepos.Init(target.root)
}

func (h *Handler) planMeta(cmd *command.MoveGraphCmd, s, t *moveRepo, same bool) error {
	metaPath := filepath.Join(model.SDDDirName, model.SchemaMetaFileName)
	data, err := os.ReadFile(filepath.Join(s.root, metaPath))
	if err != nil {
		return fmt.Errorf("reading the graph's meta.json: %w", err)
	}
	schema, err := model.ParseSchemaMeta(data)
	if err != nil {
		return err
	}
	recorded := ""
	if schema.MinimumVersion != nil {
		recorded = *schema.MinimumVersion
	}
	floor := model.MoveMinimumVersion(recorded, cmd.BinaryVersion)
	schema.MinimumVersion = &floor
	formatted, err := model.FormatSchemaMeta(*schema)
	if err != nil {
		return err
	}
	t.write(metaPath, formatted)
	if !same {
		s.remove(metaPath)
	}
	return nil
}

func (h *Handler) planConfigs(ctx context.Context, target graphHome, s, t, w *moveRepo, same bool) error {
	configPath := filepath.Join(model.SDDDirName, "config.yaml")
	sourceConfig, err := os.ReadFile(filepath.Join(s.root, configPath))
	if err != nil {
		return fmt.Errorf("reading the graph's config: %w", err)
	}
	if same {
		updated, err := model.SetYAMLField(sourceConfig, "graph_dir", target.graphDir)
		if err != nil {
			return err
		}
		s.write(configPath, updated)
		return nil
	}
	workConfig, err := os.ReadFile(filepath.Join(w.root, configPath))
	if err != nil {
		return fmt.Errorf("reading this repository's config: %w", err)
	}
	agents, err := model.ParseConfig(workConfig)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", filepath.Join(w.root, configPath), err)
	}
	branch := target.location.Branch()
	if branch == "" {
		branch, err = h.projectRepos.CurrentBranch(target.root)
		if err != nil {
			return err
		}
	}
	repoID := target.clone
	if repoID == "" {
		derived, err := model.DeriveRepoID(h.projectRepos.RemoteURL(target.root))
		if err == nil {
			repoID = derived
		}
	}
	var landingAgents *model.PerRepoConfig
	if t == w {
		landingAgents = agents
	}
	projectConfig, err := model.MovedProjectConfig(sourceConfig, target.graphDir, branch, repoID, landingAgents)
	if err != nil {
		return err
	}
	t.write(configPath, projectConfig)
	if t != w {
		pointer := model.PerRepoConfig{SkillScope: agents.SkillScope, SupportedAgents: agents.SupportedAgents, Init: agents.Init}
		var formatted string
		if target.clone != "" {
			pointer.GraphRepo = target.clone
			formatted = model.FormatGraphRepoPointerConfig(pointer)
		} else {
			pointer.ProjectDir = target.location.Address
			formatted = model.FormatPointerConfig(pointer)
		}
		w.write(configPath, []byte(formatted))
	}
	if s != w && s != t {
		s.remove(configPath)
	}
	return ctx.Err()
}

// planRepoFiles plans the files init keeps beside the config that the move
// changes. Ignore and attribute files depend on no graph_dir, so only a
// repository gaining or losing its .sdd/ changes its .sdd/.gitignore; the
// graph directory's .gitattributes travels with the graph and is added when
// a graph from an earlier build lacks it.
func (h *Handler) planRepoFiles(source, target graphHome, s, t, w *moveRepo) error {
	guard := filepath.FromSlash(model.GraphElsewhereFile)
	switch {
	case target.location.Kind == model.LocationGitRepo:
		w.write(guard, []byte(model.GraphElsewhereText(target.clone)))
	case source.location.Kind == model.LocationGitRepo:
		w.remove(guard)
	}
	sddIgnore := filepath.Join(model.SDDDirName, ".gitignore")
	if t != w && t != s {
		current, err := readOptionalFile(filepath.Join(t.root, sddIgnore))
		if err != nil {
			return err
		}
		t.write(sddIgnore, model.RewriteLines(current, nil, model.SDDIgnoreLines))
	}
	if s != w && s != t {
		s.remove(sddIgnore)
	}
	graphAttributes, err := readOptionalFile(filepath.Join(source.root, source.graphDir, ".gitattributes"))
	if err != nil {
		return err
	}
	routed := model.RewriteLines(graphAttributes, nil, []string{model.GraphAttributesLine})
	if string(routed) != string(graphAttributes) {
		t.write(filepath.Join(target.graphDir, ".gitattributes"), routed)
	}
	return nil
}

func readOptionalFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return data, err
}

// sourceMovedOn fetches the source's upstream before anything is published
// and reports whether a commit there touches the graph beyond the split
// revision. An sdd-owned clone is brought up to date for the next attempt; a
// checkout is the person's to pull.
func (h *Handler) sourceMovedOn(ctx context.Context, s *moveRepo, revision string, source graphHome) (bool, error) {
	upstream, err := s.mover.UpstreamRef(ctx)
	if err != nil || upstream == "" {
		return false, err
	}
	err = h.gated(ctx, gitgate.Fetch, s.mover, s.mover.Fetch)
	if deferred, ok := deferral(err); ok {
		return false, fmt.Errorf("fetching %s before the move publishes is deferred until %s; nothing was changed, run the move again then", s.root, deferred.Next.Format("15:04"))
	}
	if err != nil {
		return false, fmt.Errorf("fetching %s before the move publishes: %w", s.root, err)
	}
	movedOn, err := s.mover.ChangedSince(ctx, revision, upstream, source.graphDir)
	if err != nil || !movedOn {
		return false, err
	}
	if source.clone == "" {
		return false, fmt.Errorf("the graph at %s moved on upstream since the move split its history; nothing was changed: pull it (sdd sync --pull), then run the move again", s.root)
	}
	err = h.gated(ctx, gitgate.Fetch, s.mover, func(ctx context.Context) error {
		_, err := s.mover.MergePull(ctx)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("bringing the clone of %s up to date: %w", source.clone, err)
	}
	return true, nil
}

// moveMachineLocal moves what this machine keeps beside the graph to where
// the new layout reads it: the session and staging stores, the usage stats
// and the machine-local config layer. The search index store stays where it
// is, reached under the new repo key through a link. oldRepo is the repo_id
// the source recorded before the move.
func (h *Handler) moveMachineLocal(cmd *command.MoveGraphCmd, project meta.Project, source, target graphHome, oldRepo model.RepoID) error {
	oldSDD, newSDD := meta.SDDDir(source.root), meta.SDDDir(target.root)
	if filepath.Clean(oldSDD) != filepath.Clean(newSDD) {
		for _, name := range []string{"sessions", "staged-blobs", "stats"} {
			err := moveTree(filepath.Join(oldSDD, name), filepath.Join(newSDD, name))
			if err != nil {
				return err
			}
		}
		if h.sessionStores != nil {
			err := h.moveSessionStores(oldSDD, newSDD, oldRepo)
			if err != nil {
				return err
			}
		}
		err := h.linkIndexStore(source, target, oldRepo)
		if err != nil {
			return err
		}
	}
	if cmd.LocalConfigOverride != "" {
		return nil
	}
	oldLocal := meta.LocalConfigPath(oldSDD, "")
	if source.clone != "" {
		oldLocal = meta.LocalConfigPath(meta.SDDDir(project.WorkRoot), "")
	}
	newLocal := meta.LocalConfigPath(newSDD, "")
	if target.clone != "" {
		newLocal = meta.LocalConfigPath(meta.SDDDir(cmd.WorkRoot), "")
	}
	if filepath.Clean(oldLocal) == filepath.Clean(newLocal) {
		return nil
	}
	from, err := os.ReadFile(oldLocal)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	into, err := os.ReadFile(newLocal)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	merged, err := model.MergeLocalConfig(into, from, model.LocalPointerKeys)
	if err != nil {
		return fmt.Errorf("moving %s: %w", oldLocal, err)
	}
	err = os.MkdirAll(filepath.Dir(newLocal), 0o755)
	if err != nil {
		return err
	}
	err = os.WriteFile(newLocal, merged, 0o644)
	if err != nil {
		return err
	}
	return os.Remove(oldLocal)
}

func (h *Handler) moveSessionStores(oldSDD, newSDD string, oldRepo model.RepoID) error {
	newRepo, err := committedRepoID(newSDD)
	if err != nil {
		return err
	}
	oldDirs, err := h.sessionStores(oldSDD, oldRepo)
	if err != nil {
		return err
	}
	newDirs, err := h.sessionStores(newSDD, newRepo)
	if err != nil {
		return err
	}
	for name, oldDir := range oldDirs {
		newDir, ok := newDirs[name]
		if !ok {
			continue
		}
		err = moveTree(oldDir, newDir)
		if err != nil {
			return err
		}
	}
	return nil
}

// linkIndexStore links the repo key the graph's search index is found under
// after the move to the store it was found under before.
func (h *Handler) linkIndexStore(source, target graphHome, oldRepo model.RepoID) error {
	if h.indexCacheRoot == "" {
		return nil
	}
	oldRoot, err := h.projectRepos.StableRoot(source.root)
	if err != nil {
		return err
	}
	newRepo, err := committedRepoID(meta.SDDDir(target.root))
	if err != nil {
		return err
	}
	newRoot, err := h.projectRepos.StableRoot(target.root)
	if err != nil {
		return err
	}
	return index.LinkKey(h.indexCacheRoot, index.RepoKey(newRepo, newRoot), index.RepoKey(oldRepo, oldRoot))
}

func committedRepoID(sddDir string) (model.RepoID, error) {
	committed, _, err := meta.ReadConfigLayers(sddDir, "")
	if err != nil || committed == nil {
		return "", err
	}
	return committed.RepoID, nil
}

// moveTree moves every file below from to the same place below to, leaving
// a file to already holds where it is; a missing from moves nothing.
func moveTree(from, to string) error {
	if filepath.Clean(from) == filepath.Clean(to) {
		return nil
	}
	_, err := os.Stat(from)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	err = filepath.WalkDir(from, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(from, file)
		if err != nil {
			return err
		}
		dest := filepath.Join(to, rel)
		_, statErr := os.Stat(dest)
		if statErr == nil {
			return fmt.Errorf("moving %s: %s exists already", file, dest)
		}
		err = os.MkdirAll(filepath.Dir(dest), 0o755)
		if err != nil {
			return err
		}
		return os.Rename(file, dest)
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(from)
}

// carryStrays carries the entries the former locations of the graph at home
// hold over to it, with the history their own repository has of them.
func (h *Handler) carryStrays(ctx context.Context, cmd *command.MoveGraphCmd, project meta.Project, home graphHome) error {
	if h.strays == nil {
		return errors.New("carrying stray entries needs the move records")
	}
	result, err := h.strays.GraphStrays(ctx, query.GraphStraysQuery{
		WorkRoot: project.WorkRoot, Repos: []string{project.WorkRoot, project.Root},
		Current: home.location, GraphDir: filepath.Join(home.root, home.graphDir), CloneDir: h.cloneDir(),
	})
	if err != nil {
		return err
	}
	if len(result.Strays) == 0 {
		return fmt.Errorf("the graph lives at %s already and no former location holds an entry it lacks; sdd move moves a graph only to a place holding none", home.location)
	}
	byLocation := map[string][]model.Stray{}
	var locations []model.LocationRef
	for _, stray := range result.Strays {
		key := stray.Location.String()
		if byLocation[key] == nil {
			locations = append(locations, stray.Location)
		}
		byLocation[key] = append(byLocation[key], stray)
	}
	current := h.moverAt(home.root)
	currentHead, err := current.Head(ctx)
	if err != nil {
		return err
	}
	for _, location := range locations {
		strays := byLocation[location.String()]
		err = h.carryFrom(ctx, cmd, project, home, current, location, strays)
		if err != nil {
			return err
		}
	}
	err = current.RecordCloneCommitsSince(ctx, currentHead)
	if err != nil {
		return err
	}
	if home.clone != "" {
		return h.graphClones.Publish(ctx, home.clone)
	}
	return nil
}

func (h *Handler) carryFrom(ctx context.Context, cmd *command.MoveGraphCmd, project meta.Project, home graphHome, current GraphMover, location model.LocationRef, strays []model.Stray) error {
	formerDir, err := finders.LocationDir(location, project.WorkRoot, h.cloneDir())
	if err != nil {
		return err
	}
	formerRoot, err := h.projectRepos.RootOf(formerDir)
	if err != nil {
		return err
	}
	if formerRoot == "" {
		formerRoot = formerDir
	}
	former := h.moverAt(formerRoot)
	revision, err := former.Head(ctx)
	if err != nil {
		return err
	}
	id, err := moveID(h.now())
	if err != nil {
		return err
	}
	record := model.MoveRecord{ID: id, From: location, To: home.location, Revision: revision, Entries: len(strays)}
	message := fmt.Sprintf("sdd: carry %d stray entries from %s to %s\n\n%s", len(strays), location, home.location, record.Trailers())
	formerPrefix, err := filepath.Rel(formerRoot, formerDir)
	if err != nil {
		return err
	}
	var formerPaths, carried []string
	for _, stray := range strays {
		rel, err := filepath.Rel(formerDir, filepath.FromSlash(stray.Path))
		if err != nil {
			return err
		}
		formerPaths = append(formerPaths, filepath.Join(formerPrefix, rel))
		carried = append(carried, filepath.Join(home.graphDir, rel))
	}
	if sameDir(formerRoot, home.root) {
		for i := range formerPaths {
			err = current.MoveTracked(ctx, formerPaths[i], carried[i])
			if err != nil {
				return err
			}
		}
		err = current.CommitAllowEmpty(ctx, message, append(slices.Clone(formerPaths), carried...)...)
	} else {
		err = h.carryAcross(ctx, former, current, formerRoot, formerPrefix, formerPaths, home, carried, message)
	}
	if err != nil {
		return err
	}
	if cmd.OnStraysCarried != nil {
		cmd.OnStraysCarried(record, strays)
	}
	return nil
}

// carryAcross replays the history the former repository has of the strays
// in the current one, copies the strays it never committed, and removes
// them from the former location; both repositories record the carry.
func (h *Handler) carryAcross(ctx context.Context, former, current GraphMover, formerRoot, formerPrefix string, formerPaths []string, home graphHome, carried []string, message string) error {
	uncommitted, err := former.Uncommitted(ctx, formerPaths)
	if err != nil {
		return err
	}
	var committed []string
	for _, p := range formerPaths {
		if !slices.Contains(uncommitted, p) {
			committed = append(committed, p)
		}
	}
	series, err := former.FileHistory(ctx, formerPrefix, committed)
	if err != nil {
		return err
	}
	err = current.ApplyHistory(ctx, series, home.graphDir)
	if err != nil {
		return err
	}
	for i, p := range formerPaths {
		dest := filepath.Join(home.root, carried[i])
		data, err := os.ReadFile(filepath.Join(formerRoot, p))
		if err != nil {
			return err
		}
		err = os.MkdirAll(filepath.Dir(dest), 0o755)
		if err != nil {
			return err
		}
		err = os.WriteFile(dest, data, 0o644)
		if err != nil {
			return err
		}
		err = os.Remove(filepath.Join(formerRoot, p))
		if err != nil {
			return err
		}
	}
	err = current.CommitAllowEmpty(ctx, message, carried...)
	if err != nil {
		return err
	}
	return former.CommitAllowEmpty(ctx, message, formerPaths...)
}

func moveID(now time.Time) (string, error) {
	var entropy [10]byte
	_, err := rand.Read(entropy[:])
	if err != nil {
		return "", fmt.Errorf("drawing a move ID: %w", err)
	}
	return model.FormatULID(now, entropy), nil
}

// entryFilesIn lists the entry-shaped files below dir.
func entryFilesIn(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(dir, file)
		if relErr == nil && !entry.IsDir() && model.EntryShaped(rel) {
			files = append(files, rel)
		}
		return nil
	})
	return files
}

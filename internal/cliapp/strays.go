package cliapp

import (
	"context"
	"errors"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/git"
	"github.com/networkteam/sdd/internal/meta"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/repos"
)

// strayRefusal is the refusal a command meets while entries lie in a former
// location of the graph governing the working directory; nil outside a
// project.
func strayRefusal(ctx context.Context) error {
	project, err := resolveProject()
	if errors.Is(err, errNoProject) {
		return nil
	}
	if err != nil {
		return err
	}
	result, err := graphStrays(ctx, project)
	if err != nil {
		return err
	}
	return result.Refusal()
}

// graphStrays lists the entries the former locations of project's graph
// hold that the graph lacks.
func graphStrays(ctx context.Context, project meta.Project) (*query.GraphStraysResult, error) {
	q, err := straysQuery(project)
	if err != nil {
		return nil, err
	}
	return finders.New(finders.Options{MoveHistory: git.MoveHistory{}}).GraphStrays(ctx, q)
}

func straysQuery(project meta.Project) (query.GraphStraysQuery, error) {
	projectCfg, err := meta.ReadConfig(project.SDDDir(), project.LocalConfigPath(localConfigOverride))
	if err != nil {
		return query.GraphStraysQuery{}, err
	}
	var pointer *model.PerRepoConfig
	if project.Pointed() {
		pointer, err = meta.ReadConfig(meta.SDDDir(project.WorkRoot), "")
		if err != nil {
			return query.GraphStraysQuery{}, err
		}
	}
	locations, err := repos.DefaultLocations()
	if err != nil {
		return query.GraphStraysQuery{}, err
	}
	return query.GraphStraysQuery{
		WorkRoot: project.WorkRoot,
		Repos:    []string{project.WorkRoot, project.Root},
		Current:  model.LayoutLocation(pointer, projectCfg),
		GraphDir: meta.ResolveGraphDir(project.Root, projectCfg),
		CloneDir: repos.NewGraphClones(locations, git.GraphClones{}, nil).Dir,
	}, nil
}

// strayGuard refuses serve's calls while entries lie in a former location of
// the graph. It reads the move records again for a new resolution of the
// config and whenever either repository's HEAD moved, which a pull or sync
// does.
type strayGuard struct {
	served  *servedProject
	heads   [2]string
	refusal error
}

func (g *strayGuard) check(ctx context.Context, project *servedProject) error {
	heads := [2]string{repoHead(ctx, project.layout.WorkRoot), repoHead(ctx, project.layout.Root)}
	if g.served == project && g.heads == heads {
		return g.refusal
	}
	result, err := graphStrays(ctx, project.layout)
	if err != nil {
		return err
	}
	g.served, g.heads, g.refusal = project, heads, result.Refusal()
	return g.refusal
}

// checksStrays reports whether the command refuses to run while strays
// exist: every one but those that set a layout up or carry strays over, the
// help, and serve, which refuses per call.
func checksStrays(command string) bool {
	switch command {
	case "", "init", "move", "serve", "help", "h", "completion":
		return false
	}
	return true
}

// refusingStraysAfter runs action, then refuses as every command does when
// what it pulled brought entries into a former location of the graph.
func refusingStraysAfter(action cli.ActionFunc) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		err := action(ctx, cmd)
		if err != nil {
			return err
		}
		return strayRefusal(ctx)
	}
}

// repoHead is the commit HEAD names in dir, empty outside a repository and
// before its first commit: the key telling a pull or sync happened.
func repoHead(ctx context.Context, dir string) string {
	head, err := git.CLI{}.HeadRevision(ctx, dir)
	if err != nil {
		return ""
	}
	return head
}

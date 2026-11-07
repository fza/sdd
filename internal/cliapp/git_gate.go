package cliapp

import (
	"context"
	"sync"

	"github.com/networkteam/sdd/internal/gitgate"
	"github.com/networkteam/sdd/internal/repos"
)

// invocationGate is the git gate of one command invocation, built on first
// use from the state root and the limits of the user-global config
// (20261107-095427-d-tac-yux).
type invocationGate struct {
	ctx  context.Context
	once sync.Once
	gate *gitgate.Gate
	err  error
}

// currentGate is replaced by the root command's Before on every run, so a
// host running commands in one process builds each run's gate from that
// run's locations and config.
var currentGate = &invocationGate{ctx: context.Background()}

// commandGate returns the invocation's git gate.
func commandGate() (*gitgate.Gate, error) {
	return currentGate.get()
}

func (g *invocationGate) get() (*gitgate.Gate, error) {
	g.once.Do(func() {
		loc, err := repos.DefaultLocations()
		if err != nil {
			g.err = err
			return
		}
		global, err := repos.LoadConfigFrom(loc.ConfigPath)
		if err != nil {
			g.err = err
			return
		}
		g.gate = gitgate.New(gitgate.Options{StateDir: loc.StateRoot, Limits: repos.GitLimitsFunc(g.ctx, global, loc.ConfigPath)})
	})
	return g.gate, g.err
}

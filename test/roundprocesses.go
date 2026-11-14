package test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// RoundProcesses stands in for the processes sdd runs launched rounds in: each
// round runs in a goroutine on an application of its own over the
// composition's stores, as the detached `sdd` process builds one. A process
// the test kills, or sdd stops, writes nothing more.
type RoundProcesses struct {
	// Idle starts processes that run no round, as one that hangs before it
	// records anything.
	Idle bool
	// BeforeStart, when set, runs as a process is about to start; an error
	// stands for a server that ended there, and no process starts.
	BeforeStart func() error

	mu       sync.Mutex
	options  sdd.ApplicationOptions
	identity sdd.RequestIdentity
	pid      int
	started  []*roundProcess
	running  sync.WaitGroup
}

type roundProcess struct {
	process  sdd.RoundProcess
	round    sdd.LaunchedRound
	sessions *ProcessSessions
	cancel   context.CancelFunc
	gone     bool
	stopped  bool
	err      error
	// groupAgent is the agent CLI a Stop of the gone process handed.
	groupAgent sdd.RoundProcess
}

// NewRoundProcesses returns processes that the test's end kills and waits for.
func NewRoundProcesses(t testing.TB) *RoundProcesses {
	p := &RoundProcesses{pid: 4000}
	t.Cleanup(func() {
		p.mu.Lock()
		for _, process := range p.started {
			p.kill(process)
		}
		p.mu.Unlock()
		p.running.Wait()
	})
	return p
}

// Runs sets the composition each round's process builds its application from,
// over its own view of options.Sessions, and the identity it runs as.
func (p *RoundProcesses) Runs(options sdd.ApplicationOptions, identity sdd.RequestIdentity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.options, p.identity = options, identity
}

func (p *RoundProcesses) Log(round sdd.LaunchedRound) string {
	return fmt.Sprintf("/round-logs/%s-round-%d.log", round.Session, round.Number)
}

func (p *RoundProcesses) Start(ctx context.Context, round sdd.LaunchedRound) (sdd.RoundProcess, error) {
	if p.BeforeStart != nil {
		if err := p.BeforeStart(); err != nil {
			return sdd.RoundProcess{}, err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.options.Sessions == nil {
		return sdd.RoundProcess{}, errors.New("the test set no composition for round processes")
	}
	p.pid++
	process := &roundProcess{
		process:  sdd.RoundProcess{PID: p.pid, Started: fmt.Sprintf("started-%d", p.pid)},
		round:    round,
		sessions: &ProcessSessions{SessionStore: p.options.Sessions},
	}
	p.started = append(p.started, process)
	if p.Idle {
		return process.process, nil
	}
	options := p.options
	options.Sessions = process.sessions
	app, err := sdd.NewApplication(options)
	if err != nil {
		return sdd.RoundProcess{}, err
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	process.cancel = cancel
	identity := p.identity
	p.running.Add(1)
	go func() {
		defer p.running.Done()
		err := app.RunLaunchedRound(runCtx, identity, round, process.process.PID)
		p.mu.Lock()
		defer p.mu.Unlock()
		process.err, process.gone = err, true
	}()
	return process.process, nil
}

func (p *RoundProcesses) Alive(_ context.Context, process sdd.RoundProcess) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	found := p.find(process.PID)
	return found != nil && found.process == process && !found.gone, nil
}

func (p *RoundProcesses) Stop(_ context.Context, process, agent sdd.RoundProcess) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	found := p.find(process.PID)
	if found == nil || found.process != process {
		return nil
	}
	if found.gone {
		found.groupAgent = agent
		return nil
	}
	p.kill(found)
	found.stopped = true
	return nil
}

// GroupStoppedThrough is the agent CLI sdd handed for the process group of
// round number of child once that round's process was gone, zero when none.
func (p *RoundProcesses) GroupStoppedThrough(child sdd.SessionID, number int) sdd.RoundProcess {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, process := range p.started {
		if process.round.Session == child && process.round.Number == number {
			return process.groupAgent
		}
	}
	return sdd.RoundProcess{}
}

// Kill ends the process of round number of child as the operating system
// would: it writes nothing more.
func (p *RoundProcesses) Kill(t testing.TB, child sdd.SessionID, number int) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, process := range p.started {
		if process.round.Session == child && process.round.Number == number {
			p.kill(process)
			return
		}
	}
	t.Fatalf("no process runs round %d of %s", number, child)
}

// Started lists the rounds a process was started for, in order.
func (p *RoundProcesses) Started() []sdd.LaunchedRound {
	p.mu.Lock()
	defer p.mu.Unlock()
	var rounds []sdd.LaunchedRound
	for _, process := range p.started {
		rounds = append(rounds, process.round)
	}
	return rounds
}

// Stopped reports whether sdd stopped the process of round number of child.
func (p *RoundProcesses) Stopped(child sdd.SessionID, number int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, process := range p.started {
		if process.round.Session == child && process.round.Number == number {
			return process.stopped
		}
	}
	return false
}

// Ended reports whether the process of round number of child ended, and the
// error it ended with.
func (p *RoundProcesses) Ended(child sdd.SessionID, number int) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, process := range p.started {
		if process.round.Session == child && process.round.Number == number {
			return process.gone, process.err
		}
	}
	return false, nil
}

func (p *RoundProcesses) find(pid int) *roundProcess {
	for _, process := range p.started {
		if process.process.PID == pid {
			return process
		}
	}
	return nil
}

func (p *RoundProcesses) kill(process *roundProcess) {
	process.sessions.Kill()
	if process.cancel != nil {
		process.cancel()
	}
	process.gone = true
}

// ProcessSessions is one process's view of a session store: once the process
// is killed, every call fails, as a process that ended does nothing more.
type ProcessSessions struct {
	sdd.SessionStore
	dead atomic.Bool
}

// errProcessEnded is what a killed process's store calls fail with.
var errProcessEnded = errors.New("the process ended")

// Kill ends the process the store belongs to.
func (s *ProcessSessions) Kill() { s.dead.Store(true) }

func (s *ProcessSessions) Create(ctx context.Context, metadata sdd.SessionMetadata) (sdd.StoredSession, error) {
	if s.dead.Load() {
		return sdd.StoredSession{}, errProcessEnded
	}
	return s.SessionStore.Create(ctx, metadata)
}

func (s *ProcessSessions) Load(ctx context.Context, id sdd.SessionID) (sdd.StoredSession, error) {
	if s.dead.Load() {
		return sdd.StoredSession{}, errProcessEnded
	}
	return s.SessionStore.Load(ctx, id)
}

func (s *ProcessSessions) List(ctx context.Context, filter sdd.SessionFilter) (sdd.SessionPage, error) {
	if s.dead.Load() {
		return sdd.SessionPage{}, errProcessEnded
	}
	return s.SessionStore.List(ctx, filter)
}

func (s *ProcessSessions) Append(ctx context.Context, id sdd.SessionID, version uint64, data sdd.SessionAppend) (uint64, error) {
	if s.dead.Load() {
		return 0, errProcessEnded
	}
	return s.SessionStore.Append(ctx, id, version, data)
}

func (s *ProcessSessions) Delete(ctx context.Context, id sdd.SessionID) error {
	if s.dead.Load() {
		return errProcessEnded
	}
	return s.SessionStore.Delete(ctx, id)
}

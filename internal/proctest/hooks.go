package proctest

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// WithHooks runs the scripted hooks at the engine's events.
func WithHooks(hooks *ScriptedHooks) Option { return func(c *config) { c.hooks = hooks } }

// HookScript is what one hook command does when it runs: wait Delay or
// until its context ends, then fail with Err or write Output to stdout. A
// Block hook waits for its context alone.
type HookScript struct {
	Delay  time.Duration
	Block  bool
	Output string
	Err    error
}

// HookRun is one recorded run: the hook, the payload it read and when it
// finished.
type HookRun struct {
	Hook     sdd.Hook
	Payload  map[string]any
	Finished time.Time
}

// ScriptedHooks is a HookCommands whose commands run scripts instead of
// processes, recording every run and allow. Configured and Scripts may
// change between calls; a changed configuration reaches the next call. A
// configured hook without a state counts as allowed; the hooks not allowed
// form one review per Origin.Checkout.
type ScriptedHooks struct {
	mu         sync.Mutex
	Configured []sdd.Hook
	Scripts    map[string]HookScript
	runs       []HookRun
	allowed    []sdd.HookReview
}

// Hooks returns the configured hooks.
func (s *ScriptedHooks) Hooks(context.Context) ([]sdd.Hook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hooks := append([]sdd.Hook(nil), s.Configured...)
	for i := range hooks {
		if hooks[i].State == "" {
			hooks[i].State = model.HookAllowed
		}
	}
	return hooks, nil
}

// Reviews groups the hooks not allowed by their checkout.
func (s *ScriptedHooks) Reviews(ctx context.Context) ([]sdd.HookReview, error) {
	hooks, err := s.Hooks(ctx)
	if err != nil {
		return nil, err
	}
	var reviews []sdd.HookReview
	at := map[string]int{}
	for _, hook := range hooks {
		if hook.State == model.HookAllowed {
			continue
		}
		i, ok := at[hook.Origin.Checkout]
		if !ok {
			i = len(reviews)
			at[hook.Origin.Checkout] = i
			reviews = append(reviews, sdd.HookReview{Checkout: hook.Origin.Checkout, State: hook.State, Hash: "scripted-" + hook.Origin.Checkout})
		}
		reviews[i].Hooks = append(reviews[i].Hooks, hook)
	}
	return reviews, nil
}

// Allow records the reviews and marks their checkouts' hooks allowed.
func (s *ScriptedHooks) Allow(_ context.Context, reviews []sdd.HookReview) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowed = append(s.allowed, reviews...)
	for _, review := range reviews {
		for i := range s.Configured {
			if s.Configured[i].Origin.Checkout == review.Checkout {
				s.Configured[i].State = model.HookAllowed
			}
		}
	}
	return nil
}

// Allowed returns the recorded allows.
func (s *ScriptedHooks) Allowed() []sdd.HookReview {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sdd.HookReview(nil), s.allowed...)
}

// Run runs the hook command's script.
func (s *ScriptedHooks) Run(ctx context.Context, hook sdd.Hook, stdin []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(stdin, &payload); err != nil {
		return nil, err
	}
	s.mu.Lock()
	script := s.Scripts[hook.Command]
	s.mu.Unlock()
	if script.Block {
		<-ctx.Done()
	} else {
		select {
		case <-time.After(script.Delay):
		case <-ctx.Done():
		}
	}
	s.mu.Lock()
	s.runs = append(s.runs, HookRun{Hook: hook, Payload: payload, Finished: time.Now()})
	s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if script.Err != nil {
		return nil, script.Err
	}
	return []byte(script.Output), nil
}

// Runs returns the recorded runs in finishing order.
func (s *ScriptedHooks) Runs() []HookRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]HookRun(nil), s.runs...)
}

// RunsAt returns the recorded runs at one event.
func (s *ScriptedHooks) RunsAt(event string) []HookRun {
	var runs []HookRun
	for _, run := range s.Runs() {
		if run.Payload["event"] == event {
			runs = append(runs, run)
		}
	}
	return runs
}

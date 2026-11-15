package secrets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/networkteam/slogutils"
	"golang.org/x/mod/semver"

	"github.com/networkteam/sdd/internal/model"
)

// Source serves the rule set scans use: the newest accepted copy the daily
// pass fetched into its folder when that copy is valid and newer than the
// built-in file, else the built-in file. A source without a folder serves the
// built-in file alone.
type Source struct {
	dir string
	log *slog.Logger

	mu      sync.Mutex
	current atomic.Pointer[sourceRules]
}

type sourceRules struct {
	set     *RuleSet
	origin  string
	tag     string
	sha256  string
	stamp   stateStamp
	problem string
}

// NewSource returns a source reading fetched copies from dir; an empty dir
// means the built-in file alone. Nothing is read before the first scan.
func NewSource(ctx context.Context, dir string) *Source {
	return &Source{dir: dir, log: slogutils.FromContext(ctx)}
}

var (
	useMu       sync.Mutex
	active      atomic.Pointer[Source]
	builtinOnly = &Source{log: slog.Default()}
)

// Active is the source the package-level scans use.
func Active() *Source {
	source := active.Load()
	if source == nil {
		return builtinOnly
	}
	return source
}

// Use points the package-level scans at the fetched copies in dir, or at the
// built-in file alone when dir is empty. Called again with the active
// source's dir, it rechecks that folder instead, which is how a long-running
// server picks up a copy fetched after it started.
func Use(ctx context.Context, dir string) {
	useMu.Lock()
	defer useMu.Unlock()
	current := Active()
	if current.dir == dir {
		current.Recheck(ctx)
		return
	}
	active.Store(NewSource(ctx, dir))
}

// Rules returns the rule set the source serves now.
func (s *Source) Rules() (*RuleSet, error) {
	loaded, err := s.loaded()
	if err != nil {
		return nil, err
	}
	return loaded.set, nil
}

func (s *Source) loaded() (*sourceRules, error) {
	current := s.current.Load()
	if current != nil {
		return current, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current = s.current.Load()
	if current != nil {
		return current, nil
	}
	current, err := s.load(nil)
	if err != nil {
		return nil, err
	}
	s.current.Store(current)
	return current, nil
}

// Recheck reads the fetched copy again when its fetch state changed since
// the source last read it. A source no scan has used yet reads it then.
func (s *Source) Recheck(ctx context.Context) {
	if s.dir == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.current.Load()
	if current == nil {
		return
	}
	stamp, err := statState(s.dir)
	if err != nil {
		slogutils.FromContext(ctx).Warn("secret rules: checking for a newly fetched rule file", "err", err)
		return
	}
	if stamp == current.stamp {
		return
	}
	next, err := s.load(current)
	if err != nil {
		slogutils.FromContext(ctx).Warn("secret rules: keeping the rules in use", "err", err)
		return
	}
	s.current.Store(next)
}

// load picks the rule set; previous, when given, is reused when the fetched
// copy it holds is still the accepted one.
func (s *Source) load(previous *sourceRules) (*sourceRules, error) {
	set, err := builtinRules()
	if err != nil {
		return nil, err
	}
	file, err := BuiltinRuleFile()
	if err != nil {
		return nil, err
	}
	loaded := &sourceRules{set: set, origin: model.SecretRulesBuiltIn, tag: file.Tag}
	if s.dir == "" {
		return loaded, nil
	}
	loaded.stamp, err = statState(s.dir)
	if err != nil {
		return nil, err
	}
	state, err := readState(s.dir)
	if err == nil && previous != nil && previous.origin == model.SecretRulesFetched && previous.tag == state.Tag && previous.sha256 == state.SHA256 {
		loaded.set, loaded.origin, loaded.tag, loaded.sha256 = previous.set, model.SecretRulesFetched, previous.tag, previous.sha256
		return loaded, nil
	}
	if err == nil {
		var fetched *RuleSet
		fetched, err = loadFetched(s.dir, state, file.Tag)
		if fetched != nil {
			loaded.set, loaded.origin, loaded.tag, loaded.sha256 = fetched, model.SecretRulesFetched, state.Tag, state.SHA256
		}
	}
	if err != nil {
		loaded.problem = err.Error()
		s.log.Warn("secret rules: using the built-in rule file", "dir", s.dir, "err", err)
	}
	return loaded, nil
}

// loadFetched reads the accepted copy state names. It returns no set when
// there is none or the built-in file is as new.
func loadFetched(dir string, state fetchState, builtinTag string) (*RuleSet, error) {
	if state.Tag == "" || semver.Compare(state.Tag, builtinTag) <= 0 {
		return nil, nil
	}
	if state.File == "" || filepath.Base(state.File) != state.File {
		return nil, fmt.Errorf("the rule fetch state names no file in %s", dir)
	}
	data, err := os.ReadFile(filepath.Join(dir, state.File))
	if err != nil {
		return nil, fmt.Errorf("reading the fetched rule file: %w", err)
	}
	// The copy was validated whole when it was accepted; a matching hash
	// proves it is that copy, so its patterns may compile on first need.
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != state.SHA256 {
		return nil, fmt.Errorf("the fetched rule file %s changed after it was accepted", state.File)
	}
	set, err := decodeRules(data)
	if err != nil {
		return nil, fmt.Errorf("fetched rule file %s: %w", state.File, err)
	}
	err = coversBuiltin(set)
	if err != nil {
		return nil, fmt.Errorf("fetched rule file %s: %w", state.File, err)
	}
	return set, nil
}

// Status reports the rules the source serves and, when it reads fetched
// copies, how fetching went.
func (s *Source) Status() (model.SecretRulesStatus, error) {
	loaded, err := s.loaded()
	if err != nil {
		return model.SecretRulesStatus{}, err
	}
	status := model.SecretRulesStatus{Update: model.SecretRulesOff, Source: loaded.origin, Tag: loaded.tag, Unused: loaded.problem}
	if s.dir == "" {
		return status, nil
	}
	status.Update = model.SecretRulesDaily
	state, err := readState(s.dir)
	if err != nil {
		return model.SecretRulesStatus{}, err
	}
	status.LastAccepted, status.LastChecked = state.AcceptedAt, state.CheckedAt
	status.LastFailure, status.LastFailureAt = state.Failure, state.FailedAt
	return status, nil
}

package model

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/networkteam/sdd/pkg/application/types"
)

// DefaultHookTimeout bounds a hook whose config names no timeout.
const DefaultHookTimeout = types.DefaultHookTimeout

// HookWildcard matches any procedure or any step in a hook's event.
const HookWildcard = "*"

// HooksDirName is the hooks folder beside each config declaring hooks: in
// .sdd/ for a repository's config files, beside the user-global config file
// for it.
const HooksDirName = "hooks"

// HookLocalDir is the subfolder of a repository's hooks folder holding the
// scripts of machine-local hooks; `sdd init` gitignores it.
const HookLocalDir = "local"

// The phases of a step's hook events: arrival, a verifier-style round, and
// the step being done.
const (
	HookPhaseStart = "start"
	HookPhaseRound = "round"
	HookPhaseDone  = "done"
)

// The hook data types live in pkg/application/types, which the exported
// application surface names (s-tac-ah2).
type (
	HookConfig = types.HookConfig
	HookLayer  = types.HookLayer
	HookOrigin = types.HookOrigin
	HookState  = types.HookState
	Hook       = types.Hook
	HookFile   = types.HookFile
	HookReview = types.HookReview
)

const (
	HookLayerGlobal    = types.HookLayerGlobal
	HookLayerCommitted = types.HookLayerCommitted
	HookLayerLocal     = types.HookLayerLocal

	HookConfirmed = types.HookConfirmed
	HookPending   = types.HookPending
	HookChanged   = types.HookChanged
)

// HookRepoWork and HookRepoProject name the two repositories of a
// project_dir layout; a project of one repository leaves the repository
// unnamed.
const (
	HookRepoWork    = "work"
	HookRepoProject = "project"
)

// hookShellChars are characters a shell would act on; a hook runs without
// one, so a command holding them was written for `sh -c`.
const hookShellChars = ";&|<>()$`\"'*?[]{}~!#"

// HookCommandPath returns the file a hook's command names, relative to its
// hooks folder: the command with escaped spaces unescaped. It refuses
// whitespace, shell characters, absolute paths and `..`, and, in a committed
// config, a script in the machine-local HookLocalDir.
func HookCommandPath(command string, layer HookLayer) (string, error) {
	var name strings.Builder
	escaped := false
	for _, r := range command {
		switch {
		case escaped:
			if r != ' ' {
				return "", fmt.Errorf("command %q escapes %q; a backslash escapes only a space in a file name", command, r)
			}
			name.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case unicode.IsSpace(r):
			return "", fmt.Errorf("command %q holds whitespace; hooks run without a shell, so arguments go in args and a space in a file name is escaped with a backslash", command)
		case strings.ContainsRune(hookShellChars, r):
			return "", fmt.Errorf("command %q holds the shell character %q; hooks run without a shell, so an interpreter goes in the script's shebang", command, r)
		default:
			name.WriteRune(r)
		}
	}
	if escaped {
		return "", fmt.Errorf("command %q ends in a backslash", command)
	}
	file := filepath.ToSlash(name.String())
	if file == "" {
		return "", errors.New("command is empty")
	}
	if strings.HasPrefix(file, "/") || filepath.IsAbs(name.String()) {
		return "", fmt.Errorf("command %q is an absolute path; name a file in the hooks folder", command)
	}
	for _, part := range strings.Split(file, "/") {
		if part == ".." {
			return "", fmt.Errorf("command %q leaves the hooks folder; name a file in it", command)
		}
	}
	file = path.Clean(file)
	if layer == HookLayerCommitted && (file == HookLocalDir || strings.HasPrefix(file, HookLocalDir+"/")) {
		return "", fmt.Errorf("command %q runs a script from %s/, which only this machine has; declare the hook in config.local.yaml", command, HookLocalDir)
	}
	return filepath.FromSlash(file), nil
}

// HookEvent is one event the running procedures raise. A step's event
// names its procedure, step and phase; a command's event leaves them
// empty. Findings marks an event whose step has a findings channel.
type HookEvent struct {
	Name      string
	Procedure string
	Step      string
	Phase     string
	Findings  bool
}

// StepHookEvent names a step's event: the bare `<procedure>.<step>` when it
// is done, else the phase appended.
func StepHookEvent(procedure, step, phase string) string {
	if phase == HookPhaseDone {
		return procedure + "." + step
	}
	return procedure + "." + step + "." + phase
}

// MatchHookEvent reports whether a hook's event pattern names the event:
// equal, or, for a step's event, the same procedure, step and phase, the
// procedure or step held as HookWildcard and a missing phase read as done.
func MatchHookEvent(pattern string, event HookEvent) bool {
	if pattern == event.Name {
		return true
	}
	if event.Step == "" {
		return false
	}
	parts := strings.Split(pattern, ".")
	phase := HookPhaseDone
	switch len(parts) {
	case 2:
	case 3:
		phase = parts[2]
	default:
		return false
	}
	return (parts[0] == HookWildcard || parts[0] == event.Procedure) &&
		(parts[1] == HookWildcard || parts[1] == event.Step) &&
		phase == event.Phase
}

// ValidateHooks checks hooks against the events the running procedures
// raise: every hook needs a timeout of zero or more, an event matching at
// least one raised event, and, when it declares findings, only events with
// a findings channel.
func ValidateHooks(hooks []Hook, events []HookEvent) error {
	var names, channels []string
	for _, event := range events {
		names = append(names, event.Name)
		if event.Findings {
			channels = append(channels, event.Name)
		}
	}
	var errs []error
	for _, hook := range hooks {
		label := fmt.Sprintf("%s: hooks[%d] (%s)", hook.Origin.ConfigPath, hook.Origin.Index, hook.Event)
		if hook.Timeout < 0 {
			errs = append(errs, fmt.Errorf("%s: timeout %d is negative", label, hook.Timeout))
		}
		var matched, withoutChannel []string
		for _, event := range events {
			if !MatchHookEvent(hook.Event, event) {
				continue
			}
			matched = append(matched, event.Name)
			if !event.Findings {
				withoutChannel = append(withoutChannel, event.Name)
			}
		}
		switch {
		case len(matched) == 0:
			errs = append(errs, fmt.Errorf("%s: unknown event %q; valid events: %s", label, hook.Event, strings.Join(names, ", ")))
		case hook.Findings && len(withoutChannel) > 0:
			errs = append(errs, fmt.Errorf("%s: declares findings, but %s has no findings channel; events with one: %s", label, strings.Join(withoutChannel, ", "), strings.Join(channels, ", ")))
		}
	}
	return errors.Join(errs...)
}

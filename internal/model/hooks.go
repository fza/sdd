package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DefaultHookTimeout bounds a hook whose config names no timeout.
const DefaultHookTimeout = 600 * time.Second

// HookWildcard matches any procedure or any step in a hook's event.
const HookWildcard = "*"

// HookConfig is one entry of the hooks list: a command sdd runs with the
// event's JSON payload on stdin (20261109-005138-d-tac-i61).
type HookConfig struct {
	// Event is `<procedure>.<step>.<entered|left|round>`, HookWildcard
	// standing for any procedure or step, or a command event such as
	// `entry.written`.
	Event string `yaml:"event"`
	// Command runs through `sh -c`.
	Command string `yaml:"command"`
	// Timeout is in seconds; zero means DefaultHookTimeout.
	Timeout int `yaml:"timeout,omitempty"`
	// Findings declares that the hook may answer with findings, which only
	// an event with a findings channel accepts.
	Findings bool `yaml:"findings,omitempty"`
}

// TimeoutDuration returns the hook's timeout, DefaultHookTimeout when unset.
func (h HookConfig) TimeoutDuration() time.Duration {
	if h.Timeout == 0 {
		return DefaultHookTimeout
	}
	return time.Duration(h.Timeout) * time.Second
}

// HookEvent is one event the running procedures raise. Findings marks an
// event whose step has a findings channel.
type HookEvent struct {
	Name     string
	Findings bool
}

// MatchHookEvent reports whether a hook's event pattern names the event:
// equal, or equal in every dot-separated part but a procedure or step part
// the pattern holds as HookWildcard. The phase never takes the wildcard.
func MatchHookEvent(pattern, event string) bool {
	if pattern == event {
		return true
	}
	patternParts := strings.Split(pattern, ".")
	eventParts := strings.Split(event, ".")
	if len(patternParts) != 3 || len(eventParts) != 3 {
		return false
	}
	for i, part := range patternParts {
		if part == eventParts[i] || (i < 2 && part == HookWildcard) {
			continue
		}
		return false
	}
	return true
}

// ValidateHooks checks configured hooks against the events the running
// procedures raise: every hook needs a command, a timeout of zero or more,
// an event matching at least one raised event, and, when it declares
// findings, only events with a findings channel.
func ValidateHooks(hooks []HookConfig, events []HookEvent) error {
	var names, channels []string
	for _, event := range events {
		names = append(names, event.Name)
		if event.Findings {
			channels = append(channels, event.Name)
		}
	}
	var errs []error
	for i, hook := range hooks {
		label := fmt.Sprintf("hooks[%d] (%s)", i, hook.Event)
		if strings.TrimSpace(hook.Command) == "" {
			errs = append(errs, fmt.Errorf("%s: command is empty", label))
		}
		if hook.Timeout < 0 {
			errs = append(errs, fmt.Errorf("%s: timeout %d is negative", label, hook.Timeout))
		}
		var matched, withoutChannel []string
		for _, event := range events {
			if !MatchHookEvent(hook.Event, event.Name) {
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

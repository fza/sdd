package types

import (
	"fmt"
	"time"
)

// DefaultHookTimeout bounds a hook whose config names no timeout.
const DefaultHookTimeout = 600 * time.Second

// HookConfig is one entry of a config file's hooks list: a file in the
// declaring layer's hooks folder sdd runs, without a shell, with the event's
// JSON payload on stdin.
type HookConfig struct {
	// Event is `<procedure>.<step>` (alias `<procedure>.<step>.done`),
	// `<procedure>.<step>.start` or `<procedure>.<step>.round`, `*` standing
	// for any procedure or step, or a command's event such as
	// `entry.written`.
	Event string `yaml:"event"`
	// Command names a file in the hooks folder; a backslash escapes a space
	// in its name.
	Command string `yaml:"command"`
	// Args are passed to the command as they stand.
	Args []string `yaml:"args,omitempty"`
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

// HookLayer names the config layer declaring a hook.
type HookLayer string

const (
	HookLayerGlobal    HookLayer = "global"
	HookLayerCommitted HookLayer = "committed"
	HookLayerLocal     HookLayer = "local"
)

// HookOrigin is where a hook is declared and where it runs.
type HookOrigin struct {
	Layer HookLayer
	// Repo is "work" or "project" in a project_dir layout, empty otherwise.
	Repo string
	// ConfigPath is the declaring config file, Index the hook's position in
	// its hooks list.
	ConfigPath string
	Index      int
	// Dir is the layer's hooks folder: the command's home and working
	// directory.
	Dir string
	// Checkout is the declaring repository's root with links resolved, the
	// key of its allow on this machine; empty for the user-global layer.
	Checkout string
}

// NeedsAllow reports whether the hook runs only once its repository's hooks
// are allowed on this machine: every layer but the user-global one.
func (o HookOrigin) NeedsAllow() bool { return o.Layer != HookLayerGlobal }

// Label names the layer, with its repository in a project_dir layout.
func (o HookOrigin) Label() string {
	if o.Repo == "" {
		return string(o.Layer)
	}
	return o.Repo + " " + string(o.Layer)
}

// HookState is whether a hook may run on this machine.
type HookState string

const (
	// HookAllowed runs: its repository's hooks are allowed as they are now,
	// or its layer needs no allow.
	HookAllowed HookState = "allowed"
	// HookPending came with a repository whose hooks were never allowed on
	// this machine.
	HookPending HookState = "pending"
	// HookChanged came with a repository whose hooks were allowed on this
	// machine before they changed.
	HookChanged HookState = "changed"
)

// Hook is a configured hook resolved against its layer: the command file
// found in the hooks folder and whether it may run on this machine.
type Hook struct {
	HookConfig
	Origin HookOrigin
	// Path is the command file's absolute path.
	Path  string
	State HookState
	// Hash is the allow hash of the hook's repository as it is now; empty for
	// the user-global layer and when sdd cannot compute it.
	Hash string
	// Notice says why a hook that is not allowed cannot be allowed as it is,
	// such as a repository git cannot list, or why nothing counts as allowed,
	// such as an unreadable allow record.
	Notice string
}

// Label names the hook in a message: its event, command and declaring file.
func (h Hook) Label() string {
	return fmt.Sprintf("%s hook %s (%s: hooks[%d])", h.Event, h.Command, h.Origin.ConfigPath, h.Origin.Index)
}

// HookFile is one file an allow covers: its slash path relative to the
// repository and its digest, the SHA-256 of its content with whether it may
// run, or a link's text.
type HookFile struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// HookReview is what a person sees before allowing one repository's hooks:
// every hook its config files declare, the files the allow covers, the head
// of each command file by its path, and the hash the allow records.
type HookReview struct {
	Repo     string
	Checkout string
	State    HookState
	Hooks    []Hook
	Files    []HookFile
	Heads    map[string][]string
	Hash     string
}

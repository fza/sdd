package types

import (
	"fmt"
	"time"
)

// DefaultHookTimeout bounds a hook whose config names no timeout.
const DefaultHookTimeout = 600 * time.Second

// HookConfig is one entry of a config file's hooks list: a file in the
// declaring layer's hooks folder sdd runs, without a shell, with the event's
// JSON payload on stdin (20261109-213250-d-tac-bve).
type HookConfig struct {
	// Event is `<procedure>.<step>` (alias `<procedure>.<step>.done`),
	// `<procedure>.<step>.start` or `<procedure>.<step>.round`, `*` standing
	// for any procedure or step, or a command's event such as
	// `entry.written`.
	Event string `yaml:"event"`
	// Command names a file in the hooks folder; a backslash escapes a space
	// in its name.
	Command string `yaml:"command"`
	// Args are passed to the command as they stand; an entry naming a file,
	// resolved from the hooks folder, joins the hook's hash.
	Args []string `yaml:"args,omitempty"`
	// Files are paths or globs, resolved from the hooks folder, whose
	// contents join the hook's hash.
	Files []string `yaml:"files,omitempty"`
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
	// Checkout is the declaring repository's root and RepoKey its key in the
	// confirmation store; both are empty for the user-global layer.
	Checkout string
	RepoKey  string
}

// NeedsConfirmation reports whether the hook runs only once its hash is
// confirmed: every layer but the user-global one.
func (o HookOrigin) NeedsConfirmation() bool { return o.Layer != HookLayerGlobal }

// Label names the layer, with its repository in a project_dir layout.
func (o HookOrigin) Label() string {
	if o.Repo == "" {
		return string(o.Layer)
	}
	return o.Repo + " " + string(o.Layer)
}

// HookState is a hook's confirmation state on this machine.
type HookState string

const (
	// HookConfirmed runs: its hash is confirmed, or its layer needs none.
	HookConfirmed HookState = "confirmed"
	// HookPending was never confirmed.
	HookPending HookState = "pending"
	// HookChanged was confirmed with another hash in this checkout.
	HookChanged HookState = "changed"
)

// Hook is a configured hook resolved against its layer: the command file
// found in the hooks folder, the hash over its config and contents, and its
// confirmation state.
type Hook struct {
	HookConfig
	Origin HookOrigin
	// Path is the command file's absolute path.
	Path  string
	Hash  string
	State HookState
}

// Label names the hook in a message: its event, command and declaring file.
func (h Hook) Label() string {
	return fmt.Sprintf("%s hook %s (%s: hooks[%d])", h.Event, h.Command, h.Origin.ConfigPath, h.Origin.Index)
}

// HookFile is one file whose contents a hook's hash covers, with its
// SHA-256 digest. Path is as the config names it, or as a glob matched it,
// relative to the hooks folder.
type HookFile struct {
	Role   string `json:"role"`
	Path   string `json:"path"`
	Digest string `json:"sha256"`
}

// HookReview is what a person sees before confirming a hook: the hook, the
// files its hash covers and the head of its command file.
type HookReview struct {
	Hook  Hook
	Files []HookFile
	Head  []string
}

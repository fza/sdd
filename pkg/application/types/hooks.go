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
}

// Label names the layer, with its repository in a project_dir layout.
func (o HookOrigin) Label() string {
	if o.Repo == "" {
		return string(o.Layer)
	}
	return o.Repo + " " + string(o.Layer)
}

// Hook is a configured hook resolved against its layer: the command file
// found in the hooks folder.
type Hook struct {
	HookConfig
	Origin HookOrigin
	// Path is the command file's absolute path.
	Path string
	// NeedsAllow marks a hook that came with the repository's committed
	// config and that this machine has not allowed, or that changed since:
	// sdd skips it until `sdd hooks allow` records it.
	NeedsAllow bool
	// Err is why the hook cannot run, such as a command file that is missing
	// or not executable. Only a call raising an event the hook matches fails
	// on it.
	Err error
}

// Label names the hook in a message: its event, command and declaring file.
func (h Hook) Label() string {
	return fmt.Sprintf("%s hook %s (%s: hooks[%d])", h.Event, h.Command, h.Origin.ConfigPath, h.Origin.Index)
}

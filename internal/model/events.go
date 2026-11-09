package model

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// EventsDirName is the directory under .sdd/ holding one directory of
// segments per event channel (20261109-004506-d-cpt-udz, D17).
const EventsDirName = "events"

// ReservedEventNamespace is the first label of the event types core writes;
// no other writer may write a type under it (20261107-115150-d-cpt-0mu).
const ReservedEventNamespace = "sdd"

// EventShare is how a channel's segments travel (D19).
type EventShare string

const (
	// EventShareLocal keeps a channel's segments on the machine that wrote
	// them.
	EventShareLocal EventShare = "local"
	// EventShareGit lets a channel's new segments ride the next sdd
	// auto-commit; the store never commits them on its own.
	EventShareGit EventShare = "git"
)

// EventsConfig is the `events` block: the sharing mode per channel. Only the
// committed .sdd/config.yaml sets it, since sharing is the team's choice.
type EventsConfig struct {
	Channels map[string]EventChannelConfig `yaml:"channels,omitempty"`
}

// EventChannelConfig configures one event channel.
type EventChannelConfig struct {
	Share EventShare `yaml:"share,omitempty"`
}

// GitChannels returns the channels shared through Git, sorted. Every channel
// the config leaves out is local.
func (c EventsConfig) GitChannels() ([]string, error) {
	var channels []string
	for name, channel := range c.Channels {
		err := ValidateEventChannel(name)
		if err != nil {
			return nil, fmt.Errorf("events.channels: %w", err)
		}
		switch channel.Share {
		case "", EventShareLocal:
		case EventShareGit:
			channels = append(channels, name)
		default:
			return nil, fmt.Errorf("events.channels.%s.share: unknown value %q (git or local)", name, channel.Share)
		}
	}
	slices.Sort(channels)
	return channels, nil
}

func mergeEventsConfig(base, overlay EventsConfig) EventsConfig {
	if len(overlay.Channels) == 0 {
		return base
	}
	channels := make(map[string]EventChannelConfig, len(base.Channels)+len(overlay.Channels))
	for name, channel := range base.Channels {
		channels[name] = channel
	}
	for name, channel := range overlay.Channels {
		if channel.Share != "" {
			channels[name] = channel
		}
	}
	return EventsConfig{Channels: channels}
}

var (
	eventLabel   = `[a-z][a-z0-9-]*`
	eventChannel = regexp.MustCompile(`^` + eventLabel + `$`)
	eventType    = regexp.MustCompile(`^` + eventLabel + `(\.` + eventLabel + `)+$`)
)

const maxEventName = 128

// ValidateEventChannel checks a channel name: one lowercase label, which is
// also its directory name.
func ValidateEventChannel(name string) error {
	if len(name) > maxEventName || !eventChannel.MatchString(name) {
		return fmt.Errorf("event channel %q: want one lowercase label of letters, digits and hyphens", name)
	}
	return nil
}

// ValidateEventType checks a type name: dotted lowercase labels, the first
// naming the writer's namespace.
func ValidateEventType(name string) error {
	if len(name) > maxEventName || !eventType.MatchString(name) {
		return fmt.Errorf("event type %q: want a namespace and a name, dotted lowercase labels such as sdd.recheck", name)
	}
	return nil
}

// EventTypeReserved reports whether a type lies in the namespace only core
// writes.
func EventTypeReserved(name string) bool {
	return strings.HasPrefix(name, ReservedEventNamespace+".")
}

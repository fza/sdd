package model

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
)

// ConnectedConfig is `connected`: whether this project may capture into a
// connected graph, as a catch-all and per target repository.
type ConnectedConfig struct {
	Write *bool                            `yaml:"write,omitempty"`
	Repos map[RepoID]ConnectedRepoOverride `yaml:"repos,omitempty"`
}

// ConnectedRepoOverride is `connected.repos.<repo-id>`.
type ConnectedRepoOverride struct {
	Write *bool `yaml:"write,omitempty"`
}

func (o ConnectedRepoOverride) String() string { return "write=" + formatOptionalBool(o.Write) }

// InboundConfig is `inbound`: whether a graph accepts captures written from
// another project, as a catch-all and per writing repository.
type InboundConfig struct {
	Accept *bool                          `yaml:"accept,omitempty"`
	Repos  map[RepoID]InboundRepoOverride `yaml:"repos,omitempty"`
}

// InboundRepoOverride is `inbound.repos.<repo-id>`.
type InboundRepoOverride struct {
	Accept *bool `yaml:"accept,omitempty"`
}

func (o InboundRepoOverride) String() string { return "accept=" + formatOptionalBool(o.Accept) }

func formatOptionalBool(value *bool) string {
	if value == nil {
		return "unset"
	}
	return strconv.FormatBool(*value)
}

// Writes reports whether the merged config lets this project capture into
// target: the target's own override, else the catch-all, else false.
func (c ConnectedConfig) Writes(target RepoID) bool {
	if override, ok := c.Repos[target]; ok && override.Write != nil {
		return *override.Write
	}
	return c.Write != nil && *c.Write
}

// Accepts reports whether the graph accepts captures written from writer:
// the writer's own override, else the catch-all, else false.
func (c InboundConfig) Accepts(writer RepoID) bool {
	if override, ok := c.Repos[writer]; ok && override.Accept != nil {
		return *override.Accept
	}
	return c.Accept != nil && *c.Accept
}

// ErrConnectedWriteRefused reports a capture into a connected graph that
// one of the two gates does not allow.
var ErrConnectedWriteRefused = errors.New("capture into a connected graph refused")

// ConnectedWriteRefusal returns why writer may not capture into target, nil
// when both gates allow it: writing's merged `connected` config and the
// target's committed `inbound` config.
func ConnectedWriteRefusal(writer RepoID, writing ConnectedConfig, target RepoID, receiving InboundConfig) error {
	if !writing.Writes(target) {
		return fmt.Errorf("%w: connected.write does not allow writing into %s; set it with `sdd config set --repo %s connected.write true`", ErrConnectedWriteRefused, target, target)
	}
	if !receiving.Accepts(writer) {
		return fmt.Errorf("%w: %s does not accept captures from %s; its committed .sdd/config.yaml needs inbound.accept or inbound.repos.%s.accept set to true", ErrConnectedWriteRefused, target, writer, writer)
	}
	return nil
}

// RepoOverrideKey is the key path `sdd config set --repo` writes: an
// override map's entry for repoID, which a dotted path cannot address since
// the id holds dots.
func RepoOverrideKey(key string, repoID RepoID) ([]string, error) {
	if err := ValidateRepoID(repoID); err != nil {
		return nil, err
	}
	switch key {
	case "connected.write":
		return []string{"connected", "repos", string(repoID), "write"}, nil
	case "inbound.accept":
		return []string{"inbound", "repos", string(repoID), "accept"}, nil
	}
	return nil, fmt.Errorf("%q takes no per-repository override; --repo serves connected.write", key)
}

// HonoredLocal returns the machine-local layer without inbound, events and
// relevance, which only the committed config sets, and without the git
// limits, which only the user-global config sets.
func HonoredLocal(local *PerRepoConfig) *PerRepoConfig {
	if local == nil || (local.Inbound.Accept == nil && len(local.Inbound.Repos) == 0 && len(local.Events.Channels) == 0 && local.Relevance == (RelevanceConfig{}) && len(misplacedGitLimits(local.Sync)) == 0) {
		return local
	}
	honored := withoutGitLimits(*local)
	honored.Inbound = InboundConfig{}
	honored.Events = EventsConfig{}
	honored.Relevance = RelevanceConfig{}
	return &honored
}

func mergeConnectedConfig(base, overlay ConnectedConfig) ConnectedConfig {
	out := base
	if overlay.Write != nil {
		out.Write = overlay.Write
	}
	if len(overlay.Repos) > 0 {
		repos := maps.Clone(base.Repos)
		if repos == nil {
			repos = map[RepoID]ConnectedRepoOverride{}
		}
		for id, override := range overlay.Repos {
			if override.Write != nil {
				repos[id] = override
			}
		}
		out.Repos = repos
	}
	return out
}

func mergeInboundConfig(base, overlay InboundConfig) InboundConfig {
	out := base
	if overlay.Accept != nil {
		out.Accept = overlay.Accept
	}
	if len(overlay.Repos) > 0 {
		repos := maps.Clone(base.Repos)
		if repos == nil {
			repos = map[RepoID]InboundRepoOverride{}
		}
		for id, override := range overlay.Repos {
			if override.Accept != nil {
				repos[id] = override
			}
		}
		out.Repos = repos
	}
	return out
}

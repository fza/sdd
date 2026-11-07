package model

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Built-in git limits, the same for every remote host, and the floors below
// which a configured cooldown is ignored (20261107-095427-d-tac-yux).
const (
	DefaultGitFetchCooldown               = 5 * time.Minute
	DefaultGitPushCooldown                = 30 * time.Second
	DefaultGitRetries                     = 2
	DefaultGitConcurrency                 = 2
	DefaultGitSSHPerHour                  = 600
	DefaultGitHTTPSPerHour                = 600
	DefaultGitHTTPSUnauthenticatedPerHour = 60
	MinGitFetchCooldown                   = time.Minute
	MinGitPushCooldown                    = 10 * time.Second
)

const (
	globalOnlyReason    = "only the user-global config may set it"
	gitLimitsHostsKey   = "sync.hosts"
	gitLimitsDefaultKey = "sync.default"
)

// GitLimitsConfig is one block of git limits: `sync.default` or an entry of
// `sync.hosts`. An unset field leaves the value below it in force.
type GitLimitsConfig struct {
	FetchCooldown string               `yaml:"fetch_cooldown,omitempty"`
	PushCooldown  string               `yaml:"push_cooldown,omitempty"`
	Retries       *int                 `yaml:"retries,omitempty"`
	Concurrency   *int                 `yaml:"concurrency,omitempty"`
	SSH           GitSSHLimitsConfig   `yaml:"ssh,omitempty"`
	HTTPS         GitHTTPSLimitsConfig `yaml:"https,omitempty"`
}

// GitSSHLimitsConfig is the `ssh` block of a GitLimitsConfig.
type GitSSHLimitsConfig struct {
	PerHour *int `yaml:"per_hour,omitempty"`
}

// GitHTTPSLimitsConfig is the `https` block of a GitLimitsConfig.
type GitHTTPSLimitsConfig struct {
	PerHour                *int `yaml:"per_hour,omitempty"`
	PerHourUnauthenticated *int `yaml:"per_hour_unauthenticated,omitempty"`
}

// GitLimits are the resolved limits on git network operations against one
// remote host.
type GitLimits struct {
	FetchCooldown               time.Duration
	PushCooldown                time.Duration
	Retries                     int
	Concurrency                 int
	SSHPerHour                  int
	HTTPSPerHour                int
	HTTPSUnauthenticatedPerHour int
}

// DefaultGitLimits returns the built-in limits.
func DefaultGitLimits() GitLimits {
	return GitLimits{
		FetchCooldown:               DefaultGitFetchCooldown,
		PushCooldown:                DefaultGitPushCooldown,
		Retries:                     DefaultGitRetries,
		Concurrency:                 DefaultGitConcurrency,
		SSHPerHour:                  DefaultGitSSHPerHour,
		HTTPSPerHour:                DefaultGitHTTPSPerHour,
		HTTPSUnauthenticatedPerHour: DefaultGitHTTPSUnauthenticatedPerHour,
	}
}

// ResolveGitLimits returns the limits for host: the built-in defaults, then
// sync.default, then the sync.hosts entry for host. A value that is malformed,
// not positive or below its floor is ignored and named in a warning.
func ResolveGitLimits(sync SyncConfig, host string) (GitLimits, []string) {
	limits := DefaultGitLimits()
	ignored := sync.Default.applyTo(&limits, gitLimitsDefaultKey)
	if name, block, ok := hostLimits(sync.Hosts, host); ok {
		ignored = append(ignored, block.applyTo(&limits, gitLimitsHostsKey+"."+name)...)
	}
	warnings := make([]string, 0, len(ignored))
	for _, setting := range ignored {
		warnings = append(warnings, setting.Key+": "+setting.Reason)
	}
	return limits, warnings
}

// IgnoredGitLimits returns every git limit value in sync that resolution
// ignores, across sync.default and all sync.hosts entries.
func IgnoredGitLimits(sync SyncConfig) []IgnoredSetting {
	var scratch GitLimits
	ignored := sync.Default.applyTo(&scratch, gitLimitsDefaultKey)
	hosts := make([]string, 0, len(sync.Hosts))
	for name := range sync.Hosts {
		hosts = append(hosts, name)
	}
	slices.Sort(hosts)
	for _, name := range hosts {
		ignored = append(ignored, sync.Hosts[name].applyTo(&scratch, gitLimitsHostsKey+"."+name)...)
	}
	return ignored
}

// SyncHostsKeyPath splits a `sync.hosts.<host>.<setting>` key into its YAML
// path. A host name holds dots, so the setting is matched from the end.
// ok is false for a key outside sync.hosts.
func SyncHostsKeyPath(key string) (path []string, ok bool, err error) {
	rest, ok := strings.CutPrefix(key, gitLimitsHostsKey+".")
	if !ok {
		return nil, false, nil
	}
	settings := gitLimitsSettings()
	for _, setting := range settings {
		host, found := strings.CutSuffix(rest, "."+setting)
		if found && host != "" {
			return append([]string{"sync", "hosts", host}, strings.Split(setting, ".")...), true, nil
		}
	}
	return nil, true, fmt.Errorf("%q names no git limit; use %s.<host>.<setting> with one of: %s", key, gitLimitsHostsKey, strings.Join(settings, ", "))
}

// gitLimitsSettings are the dotted keys of a GitLimitsConfig block.
func gitLimitsSettings() []string {
	leaves := collectConfigLeaves(reflect.TypeFor[GitLimitsConfig](), nil, nil)
	settings := make([]string, 0, len(leaves))
	for _, leaf := range leaves {
		settings = append(settings, leaf.path)
	}
	return settings
}

func (c GitLimitsConfig) String() string {
	var parts []string
	add := func(key, value string) {
		if value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	add("fetch_cooldown", c.FetchCooldown)
	add("push_cooldown", c.PushCooldown)
	add("retries", formatOptionalInt(c.Retries))
	add("concurrency", formatOptionalInt(c.Concurrency))
	add("ssh.per_hour", formatOptionalInt(c.SSH.PerHour))
	add("https.per_hour", formatOptionalInt(c.HTTPS.PerHour))
	add("https.per_hour_unauthenticated", formatOptionalInt(c.HTTPS.PerHourUnauthenticated))
	return strings.Join(parts, " ")
}

func formatOptionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func (c GitLimitsConfig) isZero() bool {
	return reflect.DeepEqual(c, GitLimitsConfig{})
}

// applyTo sets each valid value of c on limits and returns the values it
// ignored, keyed below prefix.
func (c GitLimitsConfig) applyTo(limits *GitLimits, prefix string) []IgnoredSetting {
	var ignored []IgnoredSetting
	reject := func(key, reason string) {
		ignored = append(ignored, IgnoredSetting{Key: prefix + "." + key, Reason: reason})
	}
	duration := func(key, raw string, floor time.Duration, target *time.Duration) {
		if raw == "" {
			return
		}
		d, err := time.ParseDuration(raw)
		switch {
		case err != nil:
			reject(key, fmt.Sprintf("%q is not a duration such as 5m", raw))
		case d < floor:
			reject(key, fmt.Sprintf("%s is below the %s minimum", raw, floor))
		default:
			*target = d
		}
	}
	count := func(key string, value *int, minimum int, target *int) {
		if value == nil {
			return
		}
		if *value < minimum {
			reject(key, fmt.Sprintf("%d is below the minimum of %d", *value, minimum))
			return
		}
		*target = *value
	}
	duration("fetch_cooldown", c.FetchCooldown, MinGitFetchCooldown, &limits.FetchCooldown)
	duration("push_cooldown", c.PushCooldown, MinGitPushCooldown, &limits.PushCooldown)
	// No retries at all is a valid choice; every other count of 0 would stop
	// all git traffic to the host.
	count("retries", c.Retries, 0, &limits.Retries)
	count("concurrency", c.Concurrency, 1, &limits.Concurrency)
	count("ssh.per_hour", c.SSH.PerHour, 1, &limits.SSHPerHour)
	count("https.per_hour", c.HTTPS.PerHour, 1, &limits.HTTPSPerHour)
	count("https.per_hour_unauthenticated", c.HTTPS.PerHourUnauthenticated, 1, &limits.HTTPSUnauthenticatedPerHour)
	return ignored
}

// hostLimits finds the sync.hosts entry for host. Host names compare without
// case, as DNS does; an exact match wins.
func hostLimits(hosts map[string]GitLimitsConfig, host string) (string, GitLimitsConfig, bool) {
	if block, ok := hosts[host]; ok {
		return host, block, true
	}
	for name, block := range hosts {
		if strings.EqualFold(name, host) {
			return name, block, true
		}
	}
	return "", GitLimitsConfig{}, false
}

// misplacedGitLimits reports the git limits a project config layer carries.
func misplacedGitLimits(sync SyncConfig) []IgnoredSetting {
	var ignored []IgnoredSetting
	if !sync.Default.isZero() {
		ignored = append(ignored, IgnoredSetting{Key: gitLimitsDefaultKey, Reason: globalOnlyReason})
	}
	if len(sync.Hosts) > 0 {
		ignored = append(ignored, IgnoredSetting{Key: gitLimitsHostsKey, Reason: globalOnlyReason})
	}
	return ignored
}

func mergeGitLimitsConfig(base, overlay GitLimitsConfig) GitLimitsConfig {
	out := base
	if overlay.FetchCooldown != "" {
		out.FetchCooldown = overlay.FetchCooldown
	}
	if overlay.PushCooldown != "" {
		out.PushCooldown = overlay.PushCooldown
	}
	if overlay.Retries != nil {
		out.Retries = overlay.Retries
	}
	if overlay.Concurrency != nil {
		out.Concurrency = overlay.Concurrency
	}
	if overlay.SSH.PerHour != nil {
		out.SSH.PerHour = overlay.SSH.PerHour
	}
	if overlay.HTTPS.PerHour != nil {
		out.HTTPS.PerHour = overlay.HTTPS.PerHour
	}
	if overlay.HTTPS.PerHourUnauthenticated != nil {
		out.HTTPS.PerHourUnauthenticated = overlay.HTTPS.PerHourUnauthenticated
	}
	return out
}

func mergeGitLimitsHosts(base, overlay map[string]GitLimitsConfig) map[string]GitLimitsConfig {
	if len(overlay) == 0 {
		return base
	}
	out := make(map[string]GitLimitsConfig, len(base)+len(overlay))
	for name, block := range base {
		out[name] = block
	}
	for name, block := range overlay {
		out[name] = mergeGitLimitsConfig(out[name], block)
	}
	return out
}

package model_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

// layerOf names the layer a personal-or-committed case reads from.
func layerOf(committed bool) model.ConfigValueSource {
	if committed {
		return model.ConfigSourceProject
	}
	return model.ConfigSourceLocal
}

func intPtr(v int) *int { return &v }

func TestResolveGitLimits(t *testing.T) {
	defaults := model.DefaultGitLimits()
	with := func(change func(*model.GitLimits)) model.GitLimits {
		limits := defaults
		change(&limits)
		return limits
	}
	cases := []struct {
		name     string
		sync     model.SyncConfig
		host     string
		want     model.GitLimits
		warnings []string
	}{
		{name: "nothing set", host: "gitlab.com", want: defaults},
		{
			name: "built-in defaults",
			want: model.GitLimits{FetchCooldown: 5 * time.Minute, PushCooldown: 30 * time.Second, Retries: 2, Concurrency: 2, SSHPerHour: 600, HTTPSPerHour: 600, HTTPSUnauthenticatedPerHour: 60},
		},
		{
			name: "default block overrides every key",
			sync: model.SyncConfig{Default: model.GitLimitsConfig{
				FetchCooldown: "10m", PushCooldown: "1m", Retries: intPtr(0), Concurrency: intPtr(4),
				SSH:   model.GitSSHLimitsConfig{PerHour: intPtr(100)},
				HTTPS: model.GitHTTPSLimitsConfig{PerHour: intPtr(200), PerHourUnauthenticated: intPtr(20)},
			}},
			host: "gitlab.com",
			want: model.GitLimits{FetchCooldown: 10 * time.Minute, PushCooldown: time.Minute, Retries: 0, Concurrency: 4, SSHPerHour: 100, HTTPSPerHour: 200, HTTPSUnauthenticatedPerHour: 20},
		},
		{
			name: "host entry overrides the default block key by key",
			sync: model.SyncConfig{
				Default: model.GitLimitsConfig{FetchCooldown: "10m", Concurrency: intPtr(4)},
				Hosts:   map[string]model.GitLimitsConfig{"gitlab.com": {FetchCooldown: "2m"}},
			},
			host: "gitlab.com",
			want: with(func(l *model.GitLimits) { l.FetchCooldown = 2 * time.Minute; l.Concurrency = 4 }),
		},
		{
			name: "another host's entry does not apply",
			sync: model.SyncConfig{Hosts: map[string]model.GitLimitsConfig{"github.com": {FetchCooldown: "2m"}}},
			host: "gitlab.com",
			want: defaults,
		},
		{
			name: "host names compare without case",
			sync: model.SyncConfig{Hosts: map[string]model.GitLimitsConfig{"GitLab.com": {Retries: intPtr(5)}}},
			host: "gitlab.com",
			want: with(func(l *model.GitLimits) { l.Retries = 5 }),
		},
		{
			name: "an exact host name wins over one differing in case",
			sync: model.SyncConfig{Hosts: map[string]model.GitLimitsConfig{"GitLab.com": {Retries: intPtr(5)}, "gitlab.com": {Retries: intPtr(1)}}},
			host: "gitlab.com",
			want: with(func(l *model.GitLimits) { l.Retries = 1 }),
		},
		{
			name:     "fetch cooldown below the floor",
			sync:     model.SyncConfig{Default: model.GitLimitsConfig{FetchCooldown: "59s"}},
			want:     defaults,
			warnings: []string{"sync.default.fetch_cooldown"},
		},
		{
			name: "fetch cooldown at the floor",
			sync: model.SyncConfig{Default: model.GitLimitsConfig{FetchCooldown: "1m"}},
			want: with(func(l *model.GitLimits) { l.FetchCooldown = time.Minute }),
		},
		{
			name:     "push cooldown below the floor",
			sync:     model.SyncConfig{Default: model.GitLimitsConfig{PushCooldown: "9s"}},
			want:     defaults,
			warnings: []string{"sync.default.push_cooldown"},
		},
		{
			name: "push cooldown at the floor",
			sync: model.SyncConfig{Default: model.GitLimitsConfig{PushCooldown: "10s"}},
			want: with(func(l *model.GitLimits) { l.PushCooldown = 10 * time.Second }),
		},
		{
			name:     "malformed duration",
			sync:     model.SyncConfig{Default: model.GitLimitsConfig{FetchCooldown: "often"}},
			want:     defaults,
			warnings: []string{"sync.default.fetch_cooldown"},
		},
		{
			name:     "negative retries",
			sync:     model.SyncConfig{Default: model.GitLimitsConfig{Retries: intPtr(-1)}},
			want:     defaults,
			warnings: []string{"sync.default.retries"},
		},
		{
			name:     "zero concurrency",
			sync:     model.SyncConfig{Default: model.GitLimitsConfig{Concurrency: intPtr(0)}},
			want:     defaults,
			warnings: []string{"sync.default.concurrency"},
		},
		{
			name: "non-positive hourly limits",
			sync: model.SyncConfig{Default: model.GitLimitsConfig{
				SSH:   model.GitSSHLimitsConfig{PerHour: intPtr(0)},
				HTTPS: model.GitHTTPSLimitsConfig{PerHour: intPtr(-5), PerHourUnauthenticated: intPtr(0)},
			}},
			want:     defaults,
			warnings: []string{"sync.default.ssh.per_hour", "sync.default.https.per_hour", "sync.default.https.per_hour_unauthenticated"},
		},
		{
			name: "an ignored host value leaves the default block's value",
			sync: model.SyncConfig{
				Default: model.GitLimitsConfig{FetchCooldown: "10m"},
				Hosts:   map[string]model.GitLimitsConfig{"gitlab.com": {FetchCooldown: "5s"}},
			},
			host:     "gitlab.com",
			want:     with(func(l *model.GitLimits) { l.FetchCooldown = 10 * time.Minute }),
			warnings: []string{"sync.hosts.gitlab.com.fetch_cooldown"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, warnings := model.ResolveGitLimits(tc.sync, tc.host)
			if got != tc.want {
				t.Errorf("each valid value should apply over the one below it, got %+v", got)
			}
			if len(warnings) != len(tc.warnings) {
				t.Fatalf("each ignored value should raise one warning, got %q", warnings)
			}
			for i, key := range tc.warnings {
				if !strings.HasPrefix(warnings[i], key+": ") || len(warnings[i]) <= len(key)+2 {
					t.Errorf("warning %q should name the key %s and why", warnings[i], key)
				}
			}
		})
	}
}

func TestIgnoredGitLimitsCoversEveryHost(t *testing.T) {
	sync := model.SyncConfig{
		Default: model.GitLimitsConfig{PushCooldown: "1s"},
		Hosts: map[string]model.GitLimitsConfig{
			"gitlab.com": {Concurrency: intPtr(0)},
			"github.com": {FetchCooldown: "1h"},
		},
	}
	var keys []string
	for _, s := range model.IgnoredGitLimits(sync) {
		keys = append(keys, s.Key)
	}
	want := []string{"sync.default.push_cooldown", "sync.hosts.gitlab.com.concurrency"}
	if !slices.Equal(keys, want) {
		t.Errorf("every ignored value of the default block and of each host should be reported in order, got %q", keys)
	}
}

func TestIgnoredSettingsReportsGitLimitsOutsideTheGlobalConfig(t *testing.T) {
	layer := model.BaseConfig{Sync: model.SyncConfig{
		Default: model.GitLimitsConfig{Retries: intPtr(3)},
		Hosts:   map[string]model.GitLimitsConfig{"gitlab.com": {}},
	}}
	for _, source := range []model.ConfigValueSource{model.ConfigSourceProject, model.ConfigSourceLocal} {
		var keys []string
		for _, s := range model.IgnoredSettings(layer, source) {
			if s.Reason == "" {
				t.Errorf("%s: %s should say why it is ignored", source, s.Key)
			}
			keys = append(keys, s.Key)
		}
		if !slices.Equal(keys, []string{"sync.default", "sync.hosts"}) {
			t.Errorf("%s layer: both git limit blocks should be reported as misplaced, got %q", source, keys)
		}
	}
	if got := model.IgnoredSettings(layer, model.ConfigSourceGlobal); len(got) != 0 {
		t.Errorf("the global config may set the git limits, got %v", got)
	}
}

func TestProjectLayersDropTheGitLimits(t *testing.T) {
	layer := func() *model.PerRepoConfig {
		return &model.PerRepoConfig{BaseConfig: model.BaseConfig{Sync: model.SyncConfig{
			Default: model.GitLimitsConfig{FetchCooldown: "10m"},
			Hosts:   map[string]model.GitLimitsConfig{"gitlab.com": {Retries: intPtr(1)}},
		}}, GraphDir: "graph"}
	}
	for name, honor := range map[string]func(*model.PerRepoConfig) *model.PerRepoConfig{
		"committed": model.HonoredCommitted,
		"local":     model.HonoredLocal,
	} {
		read := layer()
		honored := honor(read)
		if !reflect.DeepEqual(honored.Sync, model.SyncConfig{}) || honored.GraphDir != "graph" {
			t.Errorf("%s: only the git limits should be dropped, got %+v", name, honored)
		}
		if read.Sync.Default.FetchCooldown != "10m" || len(read.Sync.Hosts) != 1 {
			t.Errorf("%s: the layer read should be left as it was", name)
		}
	}
}

func TestMergeGitLimitsHostsKeyByKey(t *testing.T) {
	base := &model.PerRepoConfig{}
	base.Sync.Hosts = map[string]model.GitLimitsConfig{
		"gitlab.com": {FetchCooldown: "10m", Retries: intPtr(1)},
		"github.com": {Retries: intPtr(3)},
	}
	overlay := &model.PerRepoConfig{}
	overlay.Sync.Hosts = map[string]model.GitLimitsConfig{"gitlab.com": {Retries: intPtr(4)}}

	merged := model.MergeConfig(base, overlay).Sync.Hosts

	if got := merged["gitlab.com"]; got.FetchCooldown != "10m" || *got.Retries != 4 {
		t.Errorf("a host entry should merge key by key, got %v", got)
	}
	if got := merged["github.com"]; *got.Retries != 3 {
		t.Errorf("a host only the base sets should survive, got %v", got)
	}
	if *base.Sync.Hosts["gitlab.com"].Retries != 1 {
		t.Error("the base layer was mutated")
	}
}

func TestSyncHostsKeyPath(t *testing.T) {
	cases := []struct {
		key     string
		want    []string
		inHosts bool
		refused bool
	}{
		{key: "sync.default.retries"},
		{key: "sync.hosts.gitlab.com.retries", want: []string{"sync", "hosts", "gitlab.com", "retries"}, inHosts: true},
		{key: "sync.hosts.git.example.org.https.per_hour_unauthenticated", want: []string{"sync", "hosts", "git.example.org", "https", "per_hour_unauthenticated"}, inHosts: true},
		{key: "sync.hosts.gitlab.com.ssh.per_hour", want: []string{"sync", "hosts", "gitlab.com", "ssh", "per_hour"}, inHosts: true},
		{key: "sync.hosts.gitlab.com.typo", inHosts: true, refused: true},
		{key: "sync.hosts.retries", inHosts: true, refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			got, inHosts, err := model.SyncHostsKeyPath(tc.key)
			if inHosts != tc.inHosts {
				t.Fatalf("only a sync.hosts key should be claimed, got %t", inHosts)
			}
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), "fetch_cooldown") {
					t.Errorf("a key naming no git limit should be refused listing the settings, got %v, %v", got, err)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("the host should stay one path segment, got %q, %v", got, err)
			}
		})
	}
}

func TestGitLimitsConfigString(t *testing.T) {
	got := model.GitLimitsConfig{FetchCooldown: "2m", Retries: intPtr(0), HTTPS: model.GitHTTPSLimitsConfig{PerHourUnauthenticated: intPtr(30)}}.String()
	if got != "fetch_cooldown=2m retries=0 https.per_hour_unauthenticated=30" {
		t.Errorf("only the set keys should render, with their values, got %q", got)
	}
}

func TestEffectiveConfigShowsTheGitLimits(t *testing.T) {
	global := model.BaseConfig{Sync: model.SyncConfig{Hosts: map[string]model.GitLimitsConfig{"gitlab.com": {Retries: intPtr(1)}}}}
	values := map[string]model.ConfigValue{}
	for _, v := range model.EffectiveConfigValues(global, nil, nil) {
		values[v.Key] = v
	}
	if v := values["sync.default.fetch_cooldown"]; v.Source != model.ConfigSourceDefault || v.Value != model.DefaultGitFetchCooldown.String() {
		t.Errorf("an unset limit should show its built-in default, got %+v", v)
	}
	if v := values["sync.hosts.gitlab.com"]; v.Source != model.ConfigSourceGlobal || v.Value != "retries=1" {
		t.Errorf("a host entry should render its set keys, got %+v", v)
	}
}

package repos_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/slogutils"

	"github.com/networkteam/sdd/internal/repos"
)

func TestGitLimitsFuncResolvesPerHostAndWarnsOnce(t *testing.T) {
	cfg, err := repos.ParseGlobalConfig([]byte("sync:\n  default:\n    fetch_cooldown: 10m\n  hosts:\n    gitlab.com:\n      push_cooldown: 1s\n      retries: 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	ctx := slogutils.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))

	limitsFor := repos.GitLimitsFunc(ctx, cfg, "/home/u/.config/sdd/config.yaml")
	gitlab := limitsFor("gitlab.com")
	limitsFor("gitlab.com")
	github := limitsFor("github.com")

	if gitlab.FetchCooldown != 10*time.Minute || gitlab.Retries != 4 || gitlab.PushCooldown != 30*time.Second {
		t.Errorf("gitlab.com should take the default block, its own valid values and the built-in push cooldown, got %+v", gitlab)
	}
	if github.FetchCooldown != 10*time.Minute || github.Retries != 2 {
		t.Errorf("a host without an entry should take the default block, got %+v", github)
	}
	if n := strings.Count(logs.String(), "ignoring config setting"); n != 1 {
		t.Errorf("an ignored value should be warned about once, logged:\n%s", logs.String())
	}
	for _, part := range []string{"key=sync.hosts.gitlab.com.push_cooldown", "file=/home/u/.config/sdd/config.yaml", "level=WARN", "minimum"} {
		if !strings.Contains(logs.String(), part) {
			t.Errorf("the warning should name the key, the file and why, logged:\n%s", logs.String())
		}
	}
}

func TestGitLimitsFuncWithoutConfig(t *testing.T) {
	limits := repos.GitLimitsFunc(context.Background(), nil, "")("gitlab.com")
	if limits.FetchCooldown != 5*time.Minute || limits.Concurrency != 2 {
		t.Errorf("no global config should give the built-in limits, got %+v", limits)
	}
}

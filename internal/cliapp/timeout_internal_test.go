package cliapp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/repos"
	"github.com/networkteam/sdd/internal/secrets"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
)

// The timeout flags reach newRunner through the exported command tree only by
// a live LLM call, so the bound and the source its timeout error names are
// checked here, on the shipped flags, against a claude that outlasts them.
func TestTimeoutErrorNamesWhatSetTheBound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in binary is a shell script")
	}
	commands := []struct {
		name string
		cmd  func() *cli.Command
		flag string
	}{
		{name: "new", cmd: newCmd, flag: "preflight-timeout"},
		{name: "summarize", cmd: summarizeCmd, flag: "timeout"},
	}
	cases := []struct {
		name     string
		config   string
		flag     string
		wantName func(flag string) string
		notName  func(flag string) string
	}{
		{
			name:     "llm.timeout set",
			config:   "llm:\n  provider: claude-cli\n  timeout: 200ms\n",
			wantName: func(string) string { return "llm.timeout (200ms)" },
			notName:  func(flag string) string { return "--" + flag },
		},
		{
			name:     "flag beats llm.timeout",
			config:   "llm:\n  provider: claude-cli\n  timeout: 1m\n",
			flag:     "200ms",
			wantName: func(flag string) string { return "--" + flag + " (200ms)" },
			notName:  func(string) string { return "llm.timeout" },
		},
		{
			name:     "flag alone",
			config:   "llm:\n  provider: claude-cli\n",
			flag:     "200ms",
			wantName: func(flag string) string { return "--" + flag + " (200ms)" },
			notName:  func(string) string { return "llm.timeout" },
		},
	}
	for _, command := range commands {
		for _, tc := range cases {
			t.Run(command.name+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				bin := filepath.Join(root, "bin")
				if err := os.MkdirAll(bin, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				loc, err := repos.DefaultLocations()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(loc.ConfigPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(loc.ConfigPath, []byte(tc.config), 0o644); err != nil {
					t.Fatal(err)
				}

				var runErr error
				probe := &cli.Command{
					Name:  "probe",
					Flags: []cli.Flag{shippedFlag(t, command.cmd(), command.flag)},
					Action: func(ctx context.Context, cmd *cli.Command) error {
						runner, err := newRunner(cmd, command.flag)
						if err != nil {
							return err
						}
						_, runErr = runner.Run(secrets.WithNoNewParts(ctx), pkgllm.Request{UserPrompt: "review"})
						return nil
					},
				}
				args := []string{"probe"}
				if tc.flag != "" {
					args = append(args, "--"+command.flag, tc.flag)
				}
				if err := probe.Run(t.Context(), args); err != nil {
					t.Fatal(err)
				}
				if runErr == nil {
					t.Fatal("a call outlasting its bound should fail")
				}
				msg := runErr.Error()
				if !strings.Contains(msg, tc.wantName(command.flag)) || strings.Contains(msg, tc.notName(command.flag)) {
					t.Errorf("the timeout should name what set the bound, got %v", runErr)
				}
			})
		}
	}
}

func shippedFlag(t *testing.T, cmd *cli.Command, name string) cli.Flag {
	t.Helper()
	for _, flag := range cmd.Flags {
		if slices.Contains(flag.Names(), name) {
			return flag
		}
	}
	t.Fatalf("command %s has no flag --%s", cmd.Name, name)
	return nil
}

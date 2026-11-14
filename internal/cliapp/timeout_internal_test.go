package cliapp

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/repos"
)

// The timeout flags reach resolveTimeout through the exported command tree only
// by a live LLM call, so their fallback order is checked here: with neither the
// flag nor llm.timeout set, the provider's default must apply (zero here).
func TestTimeoutFlagsLeaveTheDefaultToTheProvider(t *testing.T) {
	commands := []struct {
		name string
		cmd  func() *cli.Command
		flag string
	}{
		{name: "new", cmd: newCmd, flag: "preflight-timeout"},
		{name: "summarize", cmd: summarizeCmd, flag: "timeout"},
	}
	cases := []struct {
		name   string
		config string
		flag   string
		want   time.Duration
	}{
		{name: "neither flag nor llm.timeout", want: 0},
		{name: "llm.timeout set", config: "llm:\n  timeout: 7m\n", want: 7 * time.Minute},
		{name: "flag beats llm.timeout", config: "llm:\n  timeout: 7m\n", flag: "30s", want: 30 * time.Second},
		{name: "flag alone", flag: "30s", want: 30 * time.Second},
	}
	for _, command := range commands {
		for _, tc := range cases {
			t.Run(command.name+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				t.Chdir(root)
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				if tc.config != "" {
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
				}

				var got time.Duration
				probe := &cli.Command{
					Name:  "probe",
					Flags: []cli.Flag{shippedFlag(t, command.cmd(), command.flag)},
					Action: func(_ context.Context, cmd *cli.Command) error {
						var err error
						got, err = resolveTimeout(cmd, command.flag)
						return err
					},
				}
				args := []string{"probe"}
				if tc.flag != "" {
					args = append(args, "--"+command.flag, tc.flag)
				}
				if err := probe.Run(t.Context(), args); err != nil {
					t.Fatal(err)
				}
				if got != tc.want {
					t.Errorf("resolveTimeout = %v, want %v", got, tc.want)
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

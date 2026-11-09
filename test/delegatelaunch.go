package test

import (
	_ "embed"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

//go:embed fixtures/delegatelaunch/fake-agent.sh
var fakeAgent []byte

// FakeAgents installs the fake agent CLI under each name in a fresh folder,
// which becomes the whole PATH, so no real agent CLI is ever found. It
// returns that folder.
func FakeAgents(t testing.TB, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		err := os.WriteFile(filepath.Join(dir, name), fakeAgent, 0o755)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("FAKE_AGENT_DIR", dir)
	return dir
}

// AgentAnswers sets what the fake agent called name prints and exits with.
func AgentAnswers(t testing.TB, dir, name, stdout, stderr string, exit int) {
	t.Helper()
	write(t, filepath.Join(dir, name+".out"), stdout)
	write(t, filepath.Join(dir, name+".stderr"), stderr)
	write(t, filepath.Join(dir, name+".exit"), strconv.Itoa(exit))
}

// AgentHangs makes the fake agent called name outlive any round timeout.
func AgentHangs(t testing.TB, dir, name string) {
	t.Helper()
	write(t, filepath.Join(dir, name+".hang"), "")
}

// AgentCall is how the fake agent was last called.
type AgentCall struct {
	Dir   string
	Args  []string
	Stdin string
}

// LastAgentCall reads how the fake agent called name was last called.
func LastAgentCall(t testing.TB, dir, name string) AgentCall {
	t.Helper()
	recorded, err := os.ReadFile(filepath.Join(dir, name+".call"))
	if err != nil {
		t.Fatal(err)
	}
	var call AgentCall
	for line := range strings.SplitSeq(strings.TrimSuffix(string(recorded), "\n"), "\n") {
		key, value, _ := strings.Cut(line, "=")
		switch key {
		case "cwd":
			call.Dir = value
		case "arg":
			call.Args = append(call.Args, value)
		}
	}
	stdin, err := os.ReadFile(filepath.Join(dir, name+".stdin"))
	if err != nil {
		t.Fatal(err)
	}
	call.Stdin = string(stdin)
	return call
}

func write(t testing.TB, path, content string) {
	t.Helper()
	err := os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

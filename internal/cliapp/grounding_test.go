package cliapp_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCLIGroundingStatusNeedsAJudge(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: false\n")
	result := f.run(t, "grounding", "status")
	if result.err == nil || !strings.Contains(result.err.Error(), "no prefilter judge") {
		t.Fatalf("sdd grounding status without a prefilter = %v, should name the missing judge", result.err)
	}
}

func TestCLIGroundingStatusReportsTheConfiguredJudge(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "judge:   systemone:jev-1.14.0 (set by `llm_prefilter`)", "no threshold yet")
}

// claudeOnPath leaves PATH holding git and, with installed, a claude that
// fails every call, so the ranking judge of a CLI command does not depend
// on the machine; it returns whether that claude ran.
func claudeOnPath(t *testing.T, f *appFixture, installed bool) func() bool {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(f.root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(f.root, "claude-ran")
	if installed {
		f.write(t, "bin/claude", "#!/bin/sh\n: > "+marker+"\necho 'claude refused' >&2\nexit 1\n")
		if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}
}

// Without an llm_prefilter block one judge ranks: Haiku chat through the
// claude CLI where a CLI command finds it, else chat on the llm provider and
// model.
func TestCLIGroundingStatusDerivesTheJudgeFromLLM(t *testing.T) {
	for _, tc := range []struct {
		name      string
		installed bool
		want      string
	}{
		{name: "with the claude CLI", installed: true, want: "judge:   chat:claude-haiku-5-5 (default on a Claude host; `llm_prefilter` would replace it)"},
		{name: "without the claude CLI", want: "judge:   chat:gpt-5.5 (default on the `llm` provider; `llm_prefilter` would replace it)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAppFixture(t)
			claudeOnPath(t, f, tc.installed)
			f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm:\n  provider: codex\n  model: gpt-5.5\n")
			result := f.run(t, "grounding", "status")
			if result.err != nil {
				t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
			}
			if !strings.Contains(result.stdout, tc.want+"\n") {
				t.Fatalf("the status should name the judge as %q:\n%s", tc.want, result.stdout)
			}
		})
	}
}

// The status opens with the labels, and --verbose reports the ranking
// judge's full calibration, no other judge's.
func TestCLIGroundingStatusVerboseReportsTheRankingJudge(t *testing.T) {
	f := newAppFixture(t)
	claudeOnPath(t, f, false)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm:\n  provider: codex\n  model: gpt-5.5\n")
	summary := f.run(t, "grounding", "status")
	if summary.err != nil || !strings.HasPrefix(summary.stdout, "labels:  0 pairs scored by people's choices\n") || strings.Contains(summary.stdout, "weights:") {
		t.Fatalf("sdd grounding status = %v, should open with the labels and leave the detail out:\n%s", summary.err, summary.stdout)
	}
	verbose := f.run(t, "grounding", "status", "--verbose")
	if verbose.err != nil {
		t.Fatalf("sdd grounding status --verbose: %v\n%s", verbose.err, verbose.stderr)
	}
	assertContainsOutput(t, "stdout", verbose.stdout, "judge: chat:gpt-5.5\n", "weights: referenced 1", "opened hidden: ", "ranking recall: ")
	if strings.Contains(verbose.stdout, "claude-haiku-5-5") {
		t.Fatalf("--verbose should report the ranking judge alone:\n%s", verbose.stdout)
	}
}

// A configured llm_prefilter ranks the deep reading too: no Claude host
// judge is reported.
func TestCLIGroundingStatusLeavesRankingToAConfiguredPrefilter(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: chat\n  model: haiku\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "judge:   chat:haiku (set by `llm_prefilter`)")
	if strings.Contains(result.stdout, "claude-haiku-5-5") {
		t.Fatalf("a configured llm_prefilter should leave no Claude host judge:\n%s", result.stdout)
	}
}

// While the recall bound does not hold, the status says the prefilter is
// uncalibrated and names the command that updates it.
func TestCLIGroundingStatusNamesTheUpdateWhileUncalibrated(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "uncalibrated: run `sdd grounding update`")
}

// serve logs once at its start that the prefilter holds no recall stage:
// that it scores the labelled pairs in the background, or, with
// verify.auto_calibrate off, the command that updates it; without a judge it
// logs nothing of it.
func TestServeLogsAnUncalibratedPrefilterOnce(t *testing.T) {
	const warning = "holds no recall stage yet"
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: false\n")
	if stderr := f.serveArgs(t, nil, nil, func(*mcp.ClientSession) {}); strings.Contains(stderr, warning) {
		t.Fatalf("serve without a prefilter judge should not warn of its calibration:\n%s", stderr)
	}
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	stderr := f.serveArgs(t, nil, nil, func(*mcp.ClientSession) {})
	if strings.Count(stderr, warning) != 1 || !strings.Contains(stderr, "scores the labelled pairs in the background") {
		t.Fatalf("serve should warn once of the uncalibrated prefilter and name its background calibration:\n%s", stderr)
	}
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n  auto_calibrate: false\n")
	stderr = f.serveArgs(t, nil, nil, func(*mcp.ClientSession) {})
	if strings.Count(stderr, warning) != 1 || !strings.Contains(stderr, "sdd grounding update") {
		t.Fatalf("serve without background calibration should warn once and name the update:\n%s", stderr)
	}
}

// serve warns of the judge the connecting client's readings rank with: the
// Claude host's for Claude Code, chat on the llm provider for any other
// client.
func TestServeWarnsOfTheJudgeTheClientRanksWith(t *testing.T) {
	const warning = "holds no recall stage yet"
	for _, tc := range []struct {
		client, judge, other string
	}{
		{client: "claude-code", judge: "judge=chat:claude-haiku-5-5", other: "chat:gpt-5.5"},
		{client: "codex-mcp-client", judge: "judge=chat:gpt-5.5", other: "claude-haiku-5-5"},
	} {
		t.Run(tc.client, func(t *testing.T) {
			f := newAppFixture(t)
			claudeOnPath(t, f, true)
			f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm:\n  provider: codex\n  model: gpt-5.5\n")
			f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n  auto_calibrate: false\n")
			f.client = tc.client
			stderr := f.serveArgs(t, nil, nil, func(*mcp.ClientSession) {})
			if strings.Count(stderr, warning) != 1 || !strings.Contains(stderr, tc.judge) || strings.Contains(stderr, tc.other) {
				t.Fatalf("serve should warn once of the judge this client ranks with:\n%s", stderr)
			}
		})
	}
}

// updateProject is a graph of two decisions referencing the fixture's
// signal, ranked by chat on an ollama listener that refuses every call and,
// with claude installed, by Haiku chat through a claude on PATH that fails
// every call. It returns how many calls reached the listener and whether
// claude ran.
func updateProject(t *testing.T, claude bool) (*appFixture, func() int, func() bool) {
	t.Helper()
	f := newAppFixture(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" && r.URL.Path != "/api/chat" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"refused"}`))
	}))
	t.Cleanup(srv.Close)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm:\n  provider: ollama\n  model: m\n  ollama_endpoint: "+srv.URL+"\n")
	for _, id := range []string{"15-020000-d-tac-one", "16-020000-d-tac-two"} {
		f.write(t, "decisions/2026/09/"+id+".md", "---\ntype: decision\nkind: directive\nlayer: tactical\nconfidence: high\nparticipants: [Local Author]\nrefs:\n    - id: 20260914-020000-s-tac-cli\n      kind: grounded-in\n      desc: the gap\nsummary: A decision on the gap.\n---\n\nThe decision.\n")
	}
	ran := claudeOnPath(t, f, claude)
	return f, func() int { return int(hits.Load()) }, ran
}

// An update labels the graph's references and scores them with the ranking
// judge alone, the Claude host's where it finds the claude CLI, else chat on
// the llm provider. A failing subject does not end the run: the judge
// scores every subject, then the failures are listed and the run fails.
func TestCLIGroundingUpdateScoresWithTheRankingJudgeThenFailsLoud(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claude bool
		judge  string
	}{
		{name: "with the claude CLI", claude: true, judge: "chat:claude-haiku-5-5"},
		{name: "without the claude CLI", judge: "chat:m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, hits, claudeRan := updateProject(t, tc.claude)
			result := f.run(t, "grounding", "update")
			if (hits() > 0) == tc.claude || claudeRan() != tc.claude {
				t.Fatalf("%d listener calls, claude ran %v: only %s should score\n%s%s", hits(), claudeRan(), tc.judge, result.stdout, result.stderr)
			}
			if result.err == nil || !strings.Contains(result.err.Error(), "2 subjects failed to score") {
				t.Fatalf("sdd grounding update = %v, should fail naming the failed subjects\n%s%s", result.err, result.stdout, result.stderr)
			}
			assertContainsOutput(t, "stdout", result.stdout, "Seeded 2 replayed reference labels from the graph's references; 0 pairs already labelled\n", tc.judge+" scored 0 labelled pairs")
			assertContainsOutput(t, "stderr", result.stderr, "failed to score, their pairs stay unscored for the next update:\n", tc.judge+" 20260915-020000-d-tac-one: ", tc.judge+" 20260916-020000-d-tac-two: ")
		})
	}
}

// --force reaches the update; no flag picks another judge.
func TestCLIGroundingUpdateTakesForceAndNoJudge(t *testing.T) {
	f, _, _ := updateProject(t, false)
	if result := f.run(t, "grounding", "update", "--force"); result.err == nil || !strings.Contains(result.err.Error(), "subjects failed to score") {
		t.Fatalf("sdd grounding update --force = %v, should score and fail on the refusing provider", result.err)
	}
	if result := f.run(t, "grounding", "update", "--judge", "chat:m"); result.err == nil || !strings.Contains(result.err.Error(), "judge") {
		t.Fatalf("sdd grounding update --judge = %v, should be refused as an unknown flag", result.err)
	}
}

func TestCLIGroundingUpdateNeedsAJudge(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: false\n")
	result := f.run(t, "grounding", "update")
	if result.err == nil || !strings.Contains(result.err.Error(), "no prefilter judge") {
		t.Fatalf("sdd grounding update without a prefilter = %v, should name the missing judge", result.err)
	}
}

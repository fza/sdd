package cliapp_test

import (
	"net/http"
	"net/http/httptest"
	"os"
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
	assertContainsOutput(t, "stdout", result.stdout, "judge: systemone:jev-1.14.0", "thresholds: none yet")
}

// Without an llm_prefilter block the prefilter is on and judges with generate
// on the llm provider and model, and the deep reading under a Claude host
// ranks with Haiku listwise, reported beside it.
func TestCLIGroundingStatusDerivesTheJudgeFromLLM(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm:\n  provider: codex\n  model: gpt-5.5\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "judge: generate:gpt-5.5", "the deep reading's ranking judge under a Claude host:\njudge: listwise:claude-haiku-5-5")
}

// A configured llm_prefilter ranks the deep reading too: no Claude host
// judge is reported.
func TestCLIGroundingStatusLeavesRankingToAConfiguredPrefilter(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: generate\n  model: haiku\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "judge: generate:haiku")
	if strings.Contains(result.stdout, "listwise") {
		t.Fatalf("a configured llm_prefilter should leave no Claude host judge:\n%s", result.stdout)
	}
}

// While the recall bound does not hold, the status says the prefilter is
// uncalibrated and names the command that seeds it.
func TestCLIGroundingStatusNamesTheSeedWhileUncalibrated(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	result := f.run(t, "grounding", "status")
	if result.err != nil {
		t.Fatalf("sdd grounding status: %v\n%s", result.err, result.stderr)
	}
	assertContainsOutput(t, "stdout", result.stdout, "uncalibrated:", "sdd grounding seed --from-refs")
}

// serve logs once at its start that the prefilter is uncalibrated, naming
// the command that seeds it; without a judge it logs nothing of it.
func TestServeLogsAnUncalibratedPrefilterOnce(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: false\n")
	if stderr := f.serveArgs(t, nil, nil, func(*mcp.ClientSession) {}); strings.Contains(stderr, "uncalibrated") {
		t.Fatalf("serve without a prefilter judge should not warn of its calibration:\n%s", stderr)
	}
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: true\n")
	f.write(t, "config/sdd/config.yaml", "participant: Global Author\nllm_prefilter:\n  protocol: systemone\n  api_keys:\n    opper: test-key\n")
	stderr := f.serveArgs(t, nil, nil, func(*mcp.ClientSession) {})
	if strings.Count(stderr, "uncalibrated") != 1 || !strings.Contains(stderr, "sdd grounding seed --from-refs") {
		t.Fatalf("serve should warn once of the uncalibrated prefilter and name the seed:\n%s", stderr)
	}
}

func TestCLIGroundingSeedTakesOneSource(t *testing.T) {
	f := newAppFixture(t)
	for _, args := range [][]string{{"grounding", "seed"}, {"grounding", "seed", "--from", "-", "--from-refs"}} {
		result := f.run(t, args...)
		if result.err == nil || !strings.Contains(result.err.Error(), "--from or --from-refs") {
			t.Errorf("sdd %v = %v, should ask for exactly one of --from and --from-refs", args, result.err)
		}
	}
}

// rescoreProject is a graph of two decisions referencing the fixture's
// signal, judged by generate on an ollama listener that refuses every call
// and, under a Claude host, by Haiku listwise through a claude on PATH that
// fails every call. It returns how many calls reached the listener and
// whether claude ran.
func rescoreProject(t *testing.T) (*appFixture, func() int, func() bool) {
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
	marker := filepath.Join(f.root, "claude-ran")
	bin := filepath.Join(f.root, "bin")
	f.write(t, "bin/claude", "#!/bin/sh\ntouch "+marker+"\necho 'claude refused' >&2\nexit 1\n")
	if err := os.Chmod(filepath.Join(bin, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ran := func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}
	return f, func() int { return int(hits.Load()) }, ran
}

// An unknown judge is refused before anything runs, naming the judges and
// their aliases.
func TestCLIGroundingRescoreRefusesAnUnknownJudge(t *testing.T) {
	f, hits, claudeRan := rescoreProject(t)
	for _, args := range [][]string{{"grounding", "rescore", "--judge", "nope"}, {"grounding", "seed", "--from-refs", "--judge", "nope"}} {
		result := f.run(t, args...)
		if result.err == nil || !strings.Contains(result.err.Error(), "generate:m (prefilter), listwise:claude-haiku-5-5 (ranking)") {
			t.Errorf("sdd %v = %v, should refuse the judge and list the available ones", args, result.err)
		}
	}
	if hits() != 0 || claudeRan() {
		t.Fatal("a refused judge should call no provider")
	}
	if result := f.run(t, "grounding", "seed", "--from", "-", "--judge", "ranking"); result.err == nil || !strings.Contains(result.err.Error(), "--judge takes --from-refs") {
		t.Errorf("sdd grounding seed --from --judge = %v, should refuse: --from scores nothing", result.err)
	}
}

// --judge scores with the one judge named: the Claude host's judge alone
// never reaches the llm provider, gather's alone never runs claude.
func TestCLIGroundingRescoreScoresWithTheNamedJudgeAlone(t *testing.T) {
	f, hits, claudeRan := rescoreProject(t)
	seeded := f.run(t, "grounding", "seed", "--from-refs", "--judge", "prefilter")
	if hits() == 0 || claudeRan() {
		t.Fatalf("%d listener calls, claude ran %v: gather's judge alone should call the llm provider and not claude\n%s%s", hits(), claudeRan(), seeded.stdout, seeded.stderr)
	}
	before := hits()
	ranked := f.run(t, "grounding", "rescore", "--judge", "ranking")
	if hits() != before || !claudeRan() {
		t.Fatalf("%d new listener calls, claude ran %v: the Claude host's judge alone should run claude and not the llm provider\n%s%s", hits()-before, claudeRan(), ranked.stdout, ranked.stderr)
	}
	if ranked.err == nil || strings.Contains(ranked.stdout, "generate:m") {
		t.Fatalf("sdd grounding rescore --judge ranking = %v:\n%s", ranked.err, ranked.stdout)
	}
}

// A failing subject does not end the run: every judge scores every subject,
// the listwise judge first, then the failures are listed and the run fails.
func TestCLIGroundingRescoreFinishesThenFailsLoud(t *testing.T) {
	f, _, _ := rescoreProject(t)
	result := f.run(t, "grounding", "seed", "--from-refs")
	if result.err == nil || !strings.Contains(result.err.Error(), "4 subjects failed to score") {
		t.Fatalf("sdd grounding seed --from-refs = %v, should fail naming the failed subjects\n%s%s", result.err, result.stdout, result.stderr)
	}
	listwise, generate := strings.Index(result.stdout, "listwise:claude-haiku-5-5 scored 0 labelled pairs"), strings.Index(result.stdout, "generate:m scored 0 labelled pairs")
	if listwise < 0 || generate < listwise {
		t.Fatalf("each judge should report, the listwise judge first:\n%s", result.stdout)
	}
	for _, want := range []string{"listwise:claude-haiku-5-5 20260915-020000-d-tac-one: ", "listwise:claude-haiku-5-5 20260916-020000-d-tac-two: ", "generate:m 20260915-020000-d-tac-one: ", "generate:m 20260916-020000-d-tac-two: "} {
		if !strings.Contains(result.stderr, want) {
			t.Errorf("the failure summary lacks %q:\n%s", want, result.stderr)
		}
	}
}

func TestCLIGroundingSeedFromRefsNeedsAJudge(t *testing.T) {
	f := newAppFixture(t)
	f.write(t, ".sdd/config.yaml", "repo_id: example.test/cli\ngraph_dir: decisions\ndefault_branch: main\nlanguage: de\nverify:\n  prefilter: false\n")
	result := f.run(t, "grounding", "seed", "--from-refs")
	if result.err == nil || !strings.Contains(result.err.Error(), "no prefilter judge") {
		t.Fatalf("sdd grounding seed --from-refs without a prefilter = %v, should name the missing judge", result.err)
	}
}

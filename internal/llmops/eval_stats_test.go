//go:build eval

// Per-call usage measurement for the eval suites (d-tac-ma6): every eval call
// records through the same Observed decorator + FileSink pair production uses,
// attributed per provider, model, and variant — never into the repo's
// .sdd/stats sink. Each call also logs one testing.T line next to the test
// that made it, and TestMain prints the run's aggregated usage table.
//
// SDD_EVAL_STATS_DIR selects the sink directory (rows append to <dir>/llm.jsonl
// across runs, so several candidate runs can accumulate into one file); unset,
// the run writes to a fresh temp dir and prints its path.

package llmops_test

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	internalllm "github.com/networkteam/sdd/internal/llm"
	"github.com/networkteam/sdd/internal/llmstats"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/pkg/llm"
)

var (
	evalSinkOnce sync.Once
	evalSinkDir  string
	evalSink     *llmstats.FileSink
	evalSinkErr  error
	evalRunStart = time.Now()
)

// evalFileSink returns the process-wide run sink, creating it on first use.
func evalFileSink(t *testing.T) internalllm.StatsSink {
	t.Helper()
	evalSinkOnce.Do(func() {
		dir := os.Getenv("SDD_EVAL_STATS_DIR")
		if dir == "" {
			dir, evalSinkErr = os.MkdirTemp("", "sdd-eval-stats-")
			if evalSinkErr != nil {
				return
			}
		}
		evalSink, evalSinkErr = llmstats.NewFileSink(dir)
		if evalSinkErr == nil {
			evalSinkDir = dir
			fmt.Printf("eval usage sink: %s/llm.jsonl\n", dir)
		}
	})
	if evalSinkErr != nil {
		t.Fatalf("creating eval stats sink: %v", evalSinkErr)
	}
	return evalSink
}

// tLogSink writes one line per call into the test log, so each call's usage
// sits next to the test that made it.
type tLogSink struct{ t *testing.T }

func (s tLogSink) RecordCall(stat internalllm.CallStat) {
	line := fmt.Sprintf("llm call: op=%s provider=%s model=%s dur=%s in=%d out=%d cache_read=%d cache_create=%d",
		stat.Purpose, stat.Identity.Provider, stat.Identity.String(),
		(time.Duration(stat.DurationMS) * time.Millisecond).Round(time.Millisecond),
		stat.Usage.InputTokens, stat.Usage.OutputTokens,
		stat.Usage.CacheReadTokens, stat.Usage.CacheCreateTokens)
	if stat.Error != "" {
		line += " error=" + stat.Error
	}
	s.t.Log(line)
}

type multiSink []internalllm.StatsSink

func (m multiSink) RecordCall(stat internalllm.CallStat) {
	for _, s := range m {
		s.RecordCall(stat)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	printEvalUsageSummary()
	os.Exit(code)
}

// printEvalUsageSummary aggregates this run's recorded calls (the sink file
// may carry earlier runs when SDD_EVAL_STATS_DIR is reused, so rows are
// filtered to the run's start).
func printEvalUsageSummary() {
	if evalSinkDir == "" {
		return
	}
	reader := llmstats.NewReader(evalSinkDir)
	records, err := reader.Read()
	if err != nil {
		fmt.Printf("eval usage summary unavailable: %v\n", err)
		return
	}
	run := model.FilterStats(records, &evalRunStart, "", "", "")
	fmt.Printf("\n=== eval LLM usage — this run (full file: %s) ===\n", reader.Path())
	presenters.RenderStatsTable(os.Stdout, &query.StatsResult{
		Report:    model.AggregateStats(run),
		Source:    reader.Path(),
		Since:     &evalRunStart,
		Until:     time.Now(),
		SinkEmpty: len(records) == 0,
	})
	printExtractionRates(run)
}

// printExtractionRates reports how often a checker's first response failed to
// parse, per identity. The rate is the reliability measure for a candidate: a
// model that never slips costs one call per check, and one that slips often
// costs two and risks failing both.
func printExtractionRates(run []model.StatsRecord) {
	type counts struct{ checks, extractions int }
	perIdentity := map[string]*counts{}
	order := []string{}

	for _, record := range run {
		identity := record.Provider + " " + record.Model
		if record.Variant != "" {
			identity += " (" + record.Variant + ")"
		}
		entry, seen := perIdentity[identity]
		if !seen {
			entry = &counts{}
			perIdentity[identity] = entry
			order = append(order, identity)
		}
		switch llm.Purpose(record.Op) {
		case llm.PurposePreflight, llm.PurposeWritingGuide:
			entry.checks++
		case llm.PurposePreflightExtract, llm.PurposeWritingGuideExtract:
			entry.extractions++
		}
	}

	fmt.Printf("\n=== verdict extraction rate — this run ===\n")
	for _, identity := range order {
		c := perIdentity[identity]
		if c.checks == 0 && c.extractions == 0 {
			continue
		}
		rate := 0.0
		if c.checks > 0 {
			rate = 100 * float64(c.extractions) / float64(c.checks)
		}
		fmt.Printf("%-48s %3d check(s), %3d extraction(s)  %5.1f%%\n", identity, c.checks, c.extractions, rate)
	}
}

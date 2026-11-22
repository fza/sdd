package application

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/llm/embed"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// Ranking configures the one ranking judge both readings of a capture use:
// gather's prefilter and the deep verifier's reading. ClaudeHost, when set,
// ranks in place of Prefilter for a session whose MCP client is a Claude host
// (model.ClaudeClient), and, while ClaudeCLI reports its claude CLI
// installed, for a reading without a client. Timeout bounds a deep ranking
// pass from its start; zero means model.DefaultRankingTimeout.
type Ranking struct {
	ClaudeHost Prefilter
	ClaudeCLI  bool
	Timeout    time.Duration
}

// rankingJudge is the judge both readings of a session under the MCP client
// clientName rank with, the empty name for a reading without a client; a nil
// Scorer for none.
func (r *ProjectRuntime) rankingJudge(clientName string) Prefilter {
	host := r.options.Ranking.ClaudeHost
	if host.Scorer != nil && (model.ClaudeClient(clientName) || clientName == "" && r.options.Ranking.ClaudeCLI) {
		return host
	}
	return r.options.Prefilter
}

func (r *ProjectRuntime) rankingTimeout() time.Duration {
	if r.options.Ranking.Timeout > 0 {
		return r.options.Ranking.Timeout
	}
	return model.DefaultRankingTimeout
}

// rankingJobTTL drops a ranking no deep reading collected, as an abandoned
// capture leaves it.
const rankingJobTTL = time.Hour

// rankingKey names the ranking of one capture's draft by one judge.
type rankingKey struct {
	session, instance, judge, subject string
}

// rankingJobs are the ranking passes of the runtime's captures, begun once a
// reading's listed set is known and collected by the deep reading.
type rankingJobs struct {
	mu   sync.Mutex
	jobs map[rankingKey]*rankingJob
}

type rankingJob struct {
	created time.Time
	mu      sync.Mutex
	covered map[string]bool
	passes  []rankingPass
	scored  scoredPairs
}

// rankingPass is one scoring of candidates: done closes once it ended, at
// the latest by deadline.
type rankingPass struct {
	done     chan struct{}
	deadline time.Time
}

// rankingRun is one pass a job owes: it scores pending, at most until the
// pass's deadline.
type rankingRun struct {
	job     *rankingJob
	pass    rankingPass
	judge   Prefilter
	subject string
	pending []relevance.Candidate
	timeout time.Duration
}

// score runs the pass and returns what it scored, which collect then finds
// in the job.
func (r *rankingRun) score(ctx context.Context) scoredPairs {
	defer close(r.pass.done)
	scored := scoreCandidates(ctx, r.judge, r.subject, r.pending, nil, r.timeout)
	r.job.mu.Lock()
	defer r.job.mu.Unlock()
	maps.Copy(r.job.scored.fresh, scored.fresh)
	r.job.scored.oversize += scored.oversize
	r.job.scored.unanswered += scored.unanswered
	for _, failure := range scored.failures {
		if !slices.Contains(r.job.scored.failures, failure) {
			r.job.scored.failures = append(r.job.scored.failures, failure)
		}
	}
	return scored
}

// abandon ends a pass that never runs.
func (r *rankingRun) abandon() { close(r.pass.done) }

// begin claims the candidates no pass of key's job covers yet as one pass,
// within timeout from now, and returns it to run; nil when every candidate
// is covered.
func (j *rankingJobs) begin(key rankingKey, judge Prefilter, subject string, candidates []relevance.Candidate, timeout time.Duration) *rankingRun {
	j.mu.Lock()
	now := time.Now()
	if j.jobs == nil {
		j.jobs = map[rankingKey]*rankingJob{}
	}
	maps.DeleteFunc(j.jobs, func(_ rankingKey, job *rankingJob) bool { return now.Sub(job.created) > rankingJobTTL })
	job := j.jobs[key]
	if job == nil {
		job = &rankingJob{created: now, covered: map[string]bool{}, scored: scoredPairs{fresh: map[string]uint8{}}}
		j.jobs[key] = job
	}
	j.mu.Unlock()

	job.mu.Lock()
	defer job.mu.Unlock()
	var pending []relevance.Candidate
	for _, candidate := range candidates {
		if !job.covered[candidate.ID] {
			job.covered[candidate.ID] = true
			pending = append(pending, candidate)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	pass := rankingPass{done: make(chan struct{}), deadline: now.Add(timeout)}
	job.passes = append(job.passes, pass)
	return &rankingRun{job: job, pass: pass, judge: judge, subject: subject, pending: pending, timeout: timeout}
}

// collect waits for every pass of key's job, each at most until its
// deadline, then takes the job and returns what its passes scored.
func (j *rankingJobs) collect(ctx context.Context, key rankingKey) scoredPairs {
	j.mu.Lock()
	job := j.jobs[key]
	delete(j.jobs, key)
	j.mu.Unlock()
	if job == nil {
		return scoredPairs{}
	}
	job.mu.Lock()
	passes := slices.Clone(job.passes)
	job.mu.Unlock()
	for _, pass := range passes {
		timer := time.NewTimer(time.Until(pass.deadline))
		select {
		case <-pass.done:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	scored := job.scored
	scored.fresh = maps.Clone(job.scored.fresh)
	scored.failures = slices.Clone(job.scored.failures)
	for _, pass := range passes {
		select {
		case <-pass.done:
		default:
			scored.late = true
		}
	}
	return scored
}

// draftSimilarity is the search index's vector score of each of ids against
// draft, read without reconciling the index; an entry the nearest hits do
// not reach has none, and nothing without an embedder.
func (r *ProjectRuntime) draftSimilarity(ctx context.Context, draft string, ids []string) (map[string]float64, error) {
	if r.options.Embedder == nil || r.options.SearchIndex == nil || len(ids) == 0 {
		return nil, nil
	}
	namespace, err := r.indexNamespace()
	if err != nil {
		return nil, err
	}
	draft, err = r.indexCapped(ctx, draft)
	if err != nil {
		return nil, err
	}
	embedded, err := r.options.Embedder.Embed(ctx, embed.Request{Purpose: embed.PurposeQuery, Texts: []string{draft}})
	if err != nil {
		return nil, err
	}
	if len(embedded.Vectors) != 1 || len(embedded.Vectors[0]) == 0 {
		return nil, fmt.Errorf("sdd: embedder returned an invalid query vector")
	}
	hits, err := r.options.SearchIndex.Nearest(ctx, []IndexNamespace{namespace}, embedded.Vectors[0], max(similarityHitsPerEntry*len(ids), similarityMinHits))
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	similarity := map[string]float64{}
	for _, hit := range hits {
		if wanted[hit.EntryID] {
			if held, ok := similarity[hit.EntryID]; !ok || hit.Score > held {
				similarity[hit.EntryID] = hit.Score
			}
		}
	}
	return similarity, nil
}

// indexCapped cuts text to the rune cap the search index's rows were
// chunked under, so a draft embeds as a query the model takes whole.
func (r *ProjectRuntime) indexCapped(ctx context.Context, text string) (string, error) {
	namespace, err := r.indexNamespace()
	if err != nil {
		return "", err
	}
	maxRunes, err := r.storedMaxRunes(ctx, namespace)
	if err != nil {
		return "", err
	}
	if len(maxRunes) > 0 && maxRunes[0] > 0 {
		if runes := []rune(text); len(runes) > maxRunes[0] {
			text = string(runes[:maxRunes[0]])
		}
	}
	return text, nil
}

// The nearest chunks a similarity read asks for: an entry owns several, so
// per entry wanted, with a floor for a short list.
const (
	similarityHitsPerEntry = 20
	similarityMinHits      = 200
)

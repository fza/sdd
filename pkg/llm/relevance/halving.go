package relevance

import (
	"context"
	"errors"
	"sync"

	"github.com/networkteam/sdd/pkg/llm"
)

// BatchSizes remembers, by key, the batch size a Halving scorer starts at.
// Lower keeps the smaller of the held size and size; nothing raises it.
type BatchSizes interface {
	Size(key string) (int, bool)
	Lower(key string, size int) error
}

// MemoryBatchSizes is a BatchSizes held in memory, so a size lasts as long
// as the process.
type MemoryBatchSizes struct {
	mu    sync.Mutex
	sizes map[string]int
}

func (m *MemoryBatchSizes) Size(key string) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	size, ok := m.sizes[key]
	return size, ok
}

func (m *MemoryBatchSizes) Lower(key string, size int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sizes == nil {
		m.sizes = map[string]int{}
	}
	if held, ok := m.sizes[key]; !ok || size < held {
		m.sizes[key] = size
	}
	return nil
}

// Halving scores a request in batches of the size sizes holds for key, the
// inner MaxBatch while it holds none. A batch whose answer breaks
// (llm.MalformedAnswerError), lacks a score (CheckResult) or runs out the
// inner call's deadline while ctx lives is scored again in halves, down to
// one candidate per call, and the half is remembered, so the next request
// starts there. Any other failure, or one of a single candidate, fails the
// request. MaxBatch reports the size the next request starts at.
func Halving(s Scorer, sizes BatchSizes, key string) Scorer {
	return halving{Scorer: s, sizes: sizes, key: key}
}

type halving struct {
	Scorer
	sizes BatchSizes
	key   string
}

func (h halving) MaxBatch() int {
	if size, ok := h.sizes.Size(h.key); ok && size > 0 {
		return size
	}
	return h.Scorer.MaxBatch()
}

func (h halving) Score(ctx context.Context, request Request) (Result, error) {
	size := h.MaxBatch()
	if size <= 0 {
		size = len(request.Candidates)
	}
	return h.split(ctx, request, size)
}

// split scores request in consecutive batches of size, or of the smaller
// size an earlier batch's halving left.
func (h halving) split(ctx context.Context, request Request, size int) (Result, error) {
	out := Result{Scores: make([]float64, 0, len(request.Candidates))}
	for start := 0; start < len(request.Candidates); start += size {
		size = min(size, h.MaxBatch())
		batch := Request{Subject: request.Subject, Candidates: request.Candidates[start:min(start+size, len(request.Candidates))]}
		result, err := h.batch(ctx, batch)
		if err != nil {
			return Result{}, err
		}
		out.Scores = append(out.Scores, result.Scores...)
		out.Identity = result.Identity
		addUsage(&out.Usage, result.Usage)
	}
	return out, nil
}

// batch scores request in one call, and in halves once that call breaks.
func (h halving) batch(ctx context.Context, request Request) (Result, error) {
	result, err := h.Scorer.Score(ctx, request)
	if err == nil {
		err = CheckResult(request, result)
		if err == nil {
			return result, nil
		}
		err = &llm.MalformedAnswerError{Err: err}
	}
	n := len(request.Candidates)
	if n == 1 || ctx.Err() != nil || !halvable(err) {
		return Result{}, err
	}
	half := n / 2
	if err := h.sizes.Lower(h.key, half); err != nil {
		return Result{}, err
	}
	return h.split(ctx, request, half)
}

// halvable is a failure a smaller batch may not repeat: a broken answer or a
// call that ran out its deadline.
func halvable(err error) bool {
	_, malformed := errors.AsType[*llm.MalformedAnswerError](err)
	return malformed || errors.Is(err, context.DeadlineExceeded)
}

func addUsage(sum *llm.Usage, usage llm.Usage) {
	sum.InputTokens += usage.InputTokens
	sum.OutputTokens += usage.OutputTokens
	sum.CacheReadTokens += usage.CacheReadTokens
	sum.CacheCreateTokens += usage.CacheCreateTokens
	sum.ReasoningTokens += usage.ReasoningTokens
	sum.CostUSD += usage.CostUSD
}

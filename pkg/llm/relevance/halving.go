package relevance

import (
	"context"
	"errors"
	"sync"

	"github.com/networkteam/sdd/pkg/llm"
)

// BatchSizes remembers, by key, the batch size a Halving scorer starts at.
// Lower keeps the smaller of the held size and size. Raise sets size while
// from is still held, so of the requests that answered at one size one
// raise lands, and none over a size lowered meanwhile.
type BatchSizes interface {
	Size(key string) (int, bool)
	Lower(key string, size int) error
	Raise(key string, from, size int) error
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

func (m *MemoryBatchSizes) Raise(key string, from, size int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if held, ok := m.sizes[key]; ok && held == from {
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
// request. A request no batch of which broke, with a batch of the
// remembered size among them, doubles that size up to the inner MaxBatch,
// so a failure that passed does not pin it; a size that breaks again halves
// again. MaxBatch reports the size the next request starts at.
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
	return h.score(ctx, request, nil)
}

// score is Score with far in every call (ScoreWithFar). A size counts the
// far candidates a call carries, which leave its own candidates that much
// less room, at least one.
func (h halving) score(ctx context.Context, request Request, far []Candidate) (Result, error) {
	held, remembered := h.sizes.Size(h.key)
	size := h.MaxBatch()
	if size <= 0 {
		size = len(request.Candidates) + len(far)
	}
	broke := false
	result, err := h.split(ctx, request, far, size, &broke)
	if err != nil {
		return Result{}, err
	}
	if grown := h.grown(held); remembered && !broke && len(request.Candidates)+len(far) >= held && grown > held {
		err = h.sizes.Raise(h.key, held, grown)
		if err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

// grown is twice size, at most the inner MaxBatch where it bounds a call.
func (h halving) grown(size int) int {
	if limit := h.Scorer.MaxBatch(); limit > 0 {
		return min(2*size, limit)
	}
	return 2 * size
}

// split scores request in consecutive batches of size, or of the smaller
// size an earlier batch's halving left, and sets broke once a batch broke.
func (h halving) split(ctx context.Context, request Request, far []Candidate, size int, broke *bool) (Result, error) {
	out := Result{Scores: make([]float64, 0, len(request.Candidates))}
	var room int
	for start := 0; start < len(request.Candidates); start += room {
		size = min(size, h.MaxBatch())
		room = size
		if len(far) > 0 {
			room = max(size-len(far), 1)
		}
		batch := Request{Subject: request.Subject, Candidates: request.Candidates[start:min(start+room, len(request.Candidates))]}
		result, err := h.batch(ctx, batch, far, broke)
		if err != nil {
			return Result{}, err
		}
		out.Scores = append(out.Scores, result.Scores...)
		out.Floor = append(out.Floor, result.Floor...)
		out.Identity = result.Identity
		addUsage(&out.Usage, result.Usage)
	}
	return out, nil
}

// batch scores request in one call, and in halves once that call breaks.
func (h halving) batch(ctx context.Context, request Request, far []Candidate, broke *bool) (Result, error) {
	call, isFar := request, []bool(nil)
	if len(far) > 0 {
		call, isFar = withFar(request, far)
	}
	result, err := h.Scorer.Score(ctx, call)
	if err == nil {
		err = CheckResult(call, result)
		if err == nil {
			if len(far) > 0 {
				result = withoutFar(result, isFar)
			}
			return result, nil
		}
		err = &llm.MalformedAnswerError{Err: err}
	}
	n := len(request.Candidates)
	if n == 1 || ctx.Err() != nil || !halvable(err) {
		return Result{}, err
	}
	*broke = true
	half := n/2 + len(far)
	if err := h.sizes.Lower(h.key, half); err != nil {
		return Result{}, err
	}
	return h.split(ctx, request, far, half, broke)
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

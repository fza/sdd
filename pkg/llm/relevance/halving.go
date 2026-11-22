package relevance

import (
	"context"
	"errors"
	"sync"

	"github.com/networkteam/sdd/pkg/llm"
)

// BatchSizes remembers, by key, the batch size a Halving scorer starts at.
// Lower keeps the smaller of the held size and size. Raise sets size while
// from is still held, so of the calls that answered at one size one raise
// lands, and none over a size lowered meanwhile. A store that outlasts its
// process hands the next one no size below MinCarriedBatch.
type BatchSizes interface {
	Size(key string) (int, bool)
	Lower(key string, size int) error
	Raise(key string, from, size int) error
}

// MinCarriedBatch is the smallest size a store hands a new process. A
// smaller one splits a pass into more calls than its deadline has time
// for, so the pass ends with few calls answered or none and the size does
// not grow back; the new process starts at the scorer's MaxBatch instead.
const MinCarriedBatch = 10

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
// request. A call answered at the remembered size doubles that size at
// once, up to the inner MaxBatch, whatever the request's other calls do, so
// a failure that passed does not pin it. A request raises nothing once one
// of its batches broke: its halves would raise the size straight back to
// the one that broke. A size that breaks again halves again. MaxBatch
// reports the size the next request starts at.
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
	size := h.MaxBatch()
	if size <= 0 {
		size = len(request.Candidates) + len(far)
	}
	broke := false
	return h.split(ctx, request, far, size, &broke)
}

// grow doubles the size remembered as from, up to the inner MaxBatch where
// it bounds a call.
func (h halving) grow(from int) error {
	size := 2 * from
	if limit := h.Scorer.MaxBatch(); limit > 0 {
		size = min(size, limit)
	}
	if size <= from {
		return nil
	}
	return h.sizes.Raise(h.key, from, size)
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
		out.Far = append(out.Far, result.Far...)
		out.Identity = result.Identity
		addUsage(&out.Usage, result.Usage)
	}
	return out, nil
}

// batch scores request in one call, and in halves once that call breaks. A
// call answered at the size remembered as it started grows that size.
func (h halving) batch(ctx context.Context, request Request, far []Candidate, broke *bool) (Result, error) {
	call, isFar := request, []bool(nil)
	if len(far) > 0 {
		call, isFar = withFar(request, far)
	}
	held, _ := h.sizes.Size(h.key)
	result, err := h.Scorer.Score(ctx, call)
	if err == nil {
		err = CheckResult(call, result)
		if err == nil {
			if !*broke && len(call.Candidates) >= held {
				err = h.grow(held)
				if err != nil {
					return Result{}, err
				}
			}
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

package relevance

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
)

// BatchSizes remembers, by key, the batch size a Halving scorer starts at.
// Lower keeps the smaller of the held size and size. Broke remembers that a
// call of size broke (BatchFailure.Noted). Raise sets size while from is
// still held and no remembered failure bars it (BatchFailure.Bars), so of
// the calls that answered at one size one raise lands, none over a size
// lowered meanwhile, and none back to a size that broke within
// FailedBatchRest. A store that outlasts its process hands the next one no
// size below MinCarriedBatch.
type BatchSizes interface {
	Size(key string) (int, bool)
	Lower(key string, size int) error
	Broke(key string, size int) error
	Raise(key string, from, size int) error
}

// FailedBatchRest is how long a raise stays below the size of a call that
// broke: long enough that a judge failing above some size pays for it once
// an hour instead of every second pass, short enough that a failure that
// passed keeps its calls small for an hour at most.
const FailedBatchRest = time.Hour

// BatchFailure is the size of a call that broke and when it last broke.
type BatchFailure struct {
	Size int       `json:"size"`
	At   time.Time `json:"at"`
}

// Bars reports whether f keeps a raise to size from landing at now: size
// reaches the size that broke, and that size has not rested FailedBatchRest.
func (f BatchFailure) Bars(size int, now time.Time) bool {
	return f.Size > 0 && size >= f.Size && now.Sub(f.At) < FailedBatchRest
}

// Noted is the failure to remember once a call of size broke at now: f
// while it bars a smaller size than size, else size at now.
func (f BatchFailure) Noted(size int, now time.Time) BatchFailure {
	if f.Size < size && f.Bars(size, now) {
		return f
	}
	return BatchFailure{Size: size, At: now}
}

// MinCarriedBatch is the smallest size a store hands a new process. A
// smaller one splits a pass into more calls than its deadline has time
// for, so the pass ends with few calls answered or none and the size does
// not grow back; the new process starts at the scorer's MaxBatch instead.
const MinCarriedBatch = 10

// MemoryBatchSizes is a BatchSizes held in memory, so a size lasts as long
// as the process.
type MemoryBatchSizes struct {
	mu       sync.Mutex
	sizes    map[string]int
	failures map[string]BatchFailure
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

func (m *MemoryBatchSizes) Broke(key string, size int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failures == nil {
		m.failures = map[string]BatchFailure{}
	}
	m.failures[key] = m.failures[key].Noted(size, time.Now())
	return nil
}

func (m *MemoryBatchSizes) Raise(key string, from, size int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failures[key].Bars(size, time.Now()) {
		return nil
	}
	if held, ok := m.sizes[key]; ok && held == from {
		m.sizes[key] = size
	}
	return nil
}

// Halving scores a request in batches of the size sizes holds for key, the
// inner MaxBatch while it holds none, and in one call while neither bounds
// it. A batch whose answer breaks
// (llm.MalformedAnswerError), lacks a score (CheckResult) or runs out the
// inner call's deadline while ctx lives is scored again in halves, down to
// one candidate per call. Under a ctx deadline a call of more than one
// candidate gets at most half the time left, so a timeout leaves room for
// its halves. The half is remembered, so the next request
// starts there. Any other failure, or one of a single candidate, fails the
// request. A call answered at the remembered size doubles that size at
// once, up to the inner MaxBatch, whatever the request's other calls do, so
// a failure that passed does not pin it. A request raises nothing once one
// of its batches broke: its halves would raise the size straight back to
// the one that broke. No raise reaches the size of a call that broke until
// that size rested FailedBatchRest; a size that breaks again then halves
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
		if limit := h.MaxBatch(); limit > 0 {
			size = min(size, limit)
		}
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

// batch scores request in one call, and in halves once that call breaks. A
// call answered at the size remembered as it started grows that size.
func (h halving) batch(ctx context.Context, request Request, far []Candidate, broke *bool) (Result, error) {
	call, isFar := request, []bool(nil)
	if len(far) > 0 {
		call, isFar = withFar(request, far)
	}
	held, _ := h.sizes.Size(h.key)
	n := len(request.Candidates)
	callCtx, cancel := callContext(ctx, n)
	result, err := h.Scorer.Score(callCtx, call)
	cancel()
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
	if n == 1 || ctx.Err() != nil || !halvable(err) {
		return Result{}, err
	}
	*broke = true
	err = h.sizes.Broke(h.key, len(call.Candidates))
	if err != nil {
		return Result{}, err
	}
	half := n/2 + len(far)
	if err := h.sizes.Lower(h.key, half); err != nil {
		return Result{}, err
	}
	return h.split(ctx, request, far, half, broke)
}

// callContext bounds a call of n candidates to half the time ctx has left,
// so a call that runs out its time leaves room for its halves before ctx
// ends. A single candidate cannot be halved and gets the whole time.
func callContext(ctx context.Context, n int) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if n == 1 || !ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Until(deadline)/2)
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

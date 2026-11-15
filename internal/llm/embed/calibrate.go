package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gofrs/flock"
	"github.com/networkteam/slogutils"

	pkgembed "github.com/networkteam/sdd/pkg/llm/embed"
)

// FallbackMaxRunes caps every input when the provider reports no token count.
const FallbackMaxRunes = 2048

// CapQuantum is the step the derived rune cap is rounded down to, so probe
// noise below one step leaves the cap, and every row hash, unchanged.
const CapQuantum = 64

const (
	latinProbeRunes    = 1000
	cjkProbeRunes      = 1000
	overflowProbeRunes = 20000
	bisectTolerance    = 64
	safetyMargin       = 0.9
	plateauFraction    = 0.9
)

// Calibration is what one serving endpoint allows: Method names how the input
// limit was found, LimitTokens is the largest accepted count, TokensPerRune is
// the worst observed ratio, and MaxRunes is the quantised cap sizing uses.
// Unverified marks a cap a control probe decided (see attempt), which the
// next load probes once more (see LoadOrCalibrate).
type Calibration struct {
	Method        string    `json:"method"`
	LimitTokens   int       `json:"limit_tokens"`
	TokensPerRune float64   `json:"tokens_per_rune"`
	MaxRunes      int       `json:"max_runes"`
	Unverified    bool      `json:"unverified,omitempty"`
	ReprobedAt    time.Time `json:"reprobed_at,omitzero"`
}

// Quantise rounds runes down to a multiple of CapQuantum.
func Quantise(runes int) int {
	return runes / CapQuantum * CapQuantum
}

// Calibrate probes emb with single-input requests and derives its rune cap.
// A provider that reports no token count gets FallbackMaxRunes. A provider
// that truncates silently shows a count stuck at its limit; one that rejects
// over-limit input is bisected between the last accepted size and the first
// rejection. A probe that gets no answer is retried once (see attempt), so a
// passing blip fails the calibration or leaves its cap unverified.
func Calibrate(ctx context.Context, emb pkgembed.Embedder) (Calibration, error) {
	latinTokens, err := shortProbe(ctx, emb, repeatRunes("word ", latinProbeRunes))
	if err != nil {
		return Calibration{}, fmt.Errorf("calibration latin probe: %w", err)
	}
	if latinTokens == 0 {
		return Calibration{Method: "unreported", MaxRunes: FallbackMaxRunes}, nil
	}
	cjkTokens, err := shortProbe(ctx, emb, repeatRunes("字", cjkProbeRunes))
	if err != nil {
		return Calibration{}, fmt.Errorf("calibration cjk probe: %w", err)
	}
	worst := math.Max(float64(latinTokens)/latinProbeRunes, float64(cjkTokens)/cjkProbeRunes)

	method, limit, unverified, err := findLimit(ctx, emb, latinTokens)
	if err != nil {
		return Calibration{}, err
	}
	maxRunes := Quantise(int(math.Floor(float64(limit) * safetyMargin / worst)))
	if maxRunes < CapQuantum {
		return Calibration{}, fmt.Errorf("calibrated limit of %d tokens leaves no room for a %d-rune cap", limit, CapQuantum)
	}
	return Calibration{Method: method, LimitTokens: limit, TokensPerRune: worst, MaxRunes: maxRunes, Unverified: unverified}, nil
}

func findLimit(ctx context.Context, emb pkgembed.Embedder, latinTokens int) (method string, limit int, unverified bool, err error) {
	overflowTokens, v, err := attempt(ctx, emb, repeatRunes("word ", overflowProbeRunes), repeatRunes("word ", latinProbeRunes))
	if err != nil {
		return "", 0, false, err
	}
	if v == accepted {
		expected := float64(overflowProbeRunes) * float64(latinTokens) / latinProbeRunes
		if float64(overflowTokens) < plateauFraction*expected {
			return "plateau", overflowTokens, false, nil
		}
		return "unbounded", overflowTokens, false, nil
	}
	unverified = v == refusedByControl
	lo, loTokens, hi := latinProbeRunes, latinTokens, overflowProbeRunes
	for hi-lo > bisectTolerance {
		mid := (lo + hi) / 2
		tokens, v, err := attempt(ctx, emb, repeatRunes("word ", mid), repeatRunes("word ", lo))
		if err != nil {
			return "", 0, false, err
		}
		if v != accepted {
			unverified = unverified || v == refusedByControl
			hi = mid
			continue
		}
		lo, loTokens = mid, tokens
	}
	return "bisect", loTokens, unverified, nil
}

// shortProbe sends a probe every endpoint must accept: a refusal fails the
// calibration.
func shortProbe(ctx context.Context, emb pkgembed.Embedder, text string) (int, error) {
	tokens, v, err := attempt(ctx, emb, text, "")
	if err != nil {
		return 0, err
	}
	if v != accepted {
		return 0, fmt.Errorf("the endpoint refused %d runes", utf8.RuneCountInString(text))
	}
	return tokens, nil
}

type verdict int

const (
	accepted verdict = iota
	refused
	refusedByControl
)

// attempt sends one probe and reads its outcome. An HTTP answer that is an
// error is a refusal. Any other failure, an unknown error included, gets the
// probe sent once more, and an answer decides. A second failure that may be the endpoint's answer to this input,
// a dropped connection (endpoints that drop over-long input) or a server
// error, counts as a refusal only when control, a size the endpoint accepted,
// is still answered: refusedByControl, as a blip on both sends reads the same.
// Any other second failure, or a failed control, leaves the outcome open,
// which fails the calibration so nothing is stored and the next fill probes
// again.
func attempt(ctx context.Context, emb pkgembed.Embedder, text, control string) (int, verdict, error) {
	tokens, err := probe(ctx, emb, text)
	if err == nil {
		return tokens, accepted, nil
	}
	if ctx.Err() != nil {
		return 0, refused, ctx.Err()
	}
	var status *statusError
	if probeFailure(err) == answered && errors.As(err, &status) {
		return 0, refused, nil
	}
	tokens, err = probe(ctx, emb, text)
	if err == nil {
		return tokens, accepted, nil
	}
	if ctx.Err() != nil {
		return 0, refused, ctx.Err()
	}
	switch probeFailure(err) {
	case answered:
		return 0, refused, nil
	case dropped:
		if control == "" {
			break
		}
		_, controlErr := probe(ctx, emb, control)
		if controlErr == nil {
			return 0, refusedByControl, nil
		}
		if ctx.Err() != nil {
			return 0, refused, ctx.Err()
		}
	}
	return 0, refused, fmt.Errorf("calibration probe of %d runes got no answer twice: %w", utf8.RuneCountInString(text), err)
}

type failure int

const (
	// answered is the endpoint's refusal of the input.
	answered failure = iota
	// dropped may be the endpoint's refusal of the input, or a blip.
	dropped
	// unanswered says nothing about the input: a timeout, a refused
	// connection, a failed lookup, a rate limit.
	unanswered
)

// probeFailure tells an endpoint's answer from a network failure: a request
// error without a transport error underneath is the endpoint's answer.
func probeFailure(err error) failure {
	var status *statusError
	if errors.As(err, &status) {
		switch {
		case status.status == http.StatusTooManyRequests:
			return unanswered
		case status.status >= http.StatusInternalServerError:
			return dropped
		}
		return answered
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return dropped
	}
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) || errors.As(err, &netErr) {
		return unanswered
	}
	return answered
}

func probe(ctx context.Context, emb pkgembed.Embedder, text string) (int, error) {
	res, err := emb.Embed(ctx, pkgembed.Request{Purpose: pkgembed.PurposeDocument, Texts: []string{text}})
	if err != nil {
		return 0, err
	}
	return res.Usage.InputTokens, nil
}

// repeatRunes returns exactly n runes made of unit repeated.
func repeatRunes(unit string, n int) string {
	runes := []rune(strings.Repeat(unit, n/len([]rune(unit))+1))
	return string(runes[:n])
}

// LoadOrCalibrate returns the calibration stored under dir for key, probing
// emb and storing the result when none exists, and probing once more when the
// stored one is unverified (see reverify). key identifies the serving endpoint
// (fingerprint, URL and path), so the vector store is never touched.
func LoadOrCalibrate(ctx context.Context, emb pkgembed.Embedder, dir, key string) (Calibration, error) {
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(dir, "calibration-"+hex.EncodeToString(sum[:])[:16]+".json")
	data, err := os.ReadFile(path)
	if err == nil {
		var cal Calibration
		if err := json.Unmarshal(data, &cal); err != nil {
			return Calibration{}, fmt.Errorf("reading calibration %s: %w", path, err)
		}
		if err := markCalibrationUsed(path, time.Now()); err != nil {
			return Calibration{}, err
		}
		if cal.Unverified {
			return reverify(ctx, emb, path, cal)
		}
		return cal, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Calibration{}, fmt.Errorf("reading calibration %s: %w", path, err)
	}
	cal, err := Calibrate(ctx, emb)
	if err != nil {
		return Calibration{}, err
	}
	if err := writeAtomic(path, cal); err != nil {
		return Calibration{}, err
	}
	return cal, nil
}

// reprobeCooldown spaces the re-probes of an unverified cap. A stamp stored
// with the calibration, rather than a memo in the process, also spaces them
// across the separate processes CLI commands run in.
const reprobeCooldown = 10 * time.Minute

// reverify probes the endpoint of an unverified calibration once more, at
// most once per reprobeCooldown and in one caller at a time: a caller that
// finds the re-probe lock held, or the cooldown running, uses stored. An open
// outcome keeps the cap unverified. A cap at least as high is stored
// verified; a lower one is stored as found.
func reverify(ctx context.Context, emb pkgembed.Embedder, path string, stored Calibration) (_ Calibration, err error) {
	now := time.Now()
	if now.Sub(stored.ReprobedAt) < reprobeCooldown {
		return stored, nil
	}
	lock := flock.New(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return Calibration{}, fmt.Errorf("locking calibration %s: %w", path, err)
	}
	if !locked {
		return stored, nil
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	current, err := readCalibration(path)
	if err != nil {
		return Calibration{}, err
	}
	if !current.Unverified || now.Sub(current.ReprobedAt) < reprobeCooldown {
		return current, nil
	}
	current.ReprobedAt = now
	err = writeAtomic(path, current)
	if err != nil {
		return Calibration{}, err
	}
	cal, err := Calibrate(ctx, emb)
	if ctx.Err() != nil {
		return Calibration{}, ctx.Err()
	}
	if err != nil {
		slogutils.FromContext(ctx).Info("calibration re-probe got no answer, keeping the unverified cap", "max_runes", current.MaxRunes, "error", err)
		return current, nil
	}
	switch {
	case cal.MaxRunes == current.MaxRunes:
		cal = current
		cal.Unverified = false
	case cal.MaxRunes > current.MaxRunes:
		cal.Unverified = false
	}
	if cal.Unverified {
		cal.ReprobedAt = now
	} else {
		cal.ReprobedAt = time.Time{}
	}
	err = writeAtomic(path, cal)
	if err != nil {
		return Calibration{}, err
	}
	return cal, nil
}

func readCalibration(path string) (Calibration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Calibration{}, fmt.Errorf("reading calibration %s: %w", path, err)
	}
	var cal Calibration
	err = json.Unmarshal(data, &cal)
	if err != nil {
		return Calibration{}, fmt.Errorf("reading calibration %s: %w", path, err)
	}
	return cal, nil
}

// CalibrationDirName is the folder under the cache root calibrations live in.
const CalibrationDirName = "calibration"

// calibrationUseRefresh is how stale a calibration's modification time, its
// last use, may grow before a load refreshes it.
const calibrationUseRefresh = 24 * time.Hour

func markCalibrationUsed(path string, now time.Time) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("reading calibration %s: %w", path, err)
	}
	if now.Sub(info.ModTime()) < calibrationUseRefresh {
		return nil
	}
	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("refreshing calibration %s: %w", path, err)
	}
	return nil
}

// PruneCalibrations removes the calibrations under dir no load used for longer
// than retention, and reports how many went.
func PruneCalibrations(dir string, now time.Time, retention time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("listing calibrations: %w", err)
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "calibration-") {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return removed, fmt.Errorf("reading calibration: %w", err)
		}
		if now.Sub(info.ModTime()) <= retention {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, fmt.Errorf("removing calibration: %w", err)
		}
		removed++
	}
	return removed, nil
}

func writeAtomic(path string, cal Calibration) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating calibration dir: %w", err)
	}
	data, err := json.Marshal(cal)
	if err != nil {
		return fmt.Errorf("encoding calibration: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".calibration-*.tmp")
	if err != nil {
		return fmt.Errorf("writing calibration: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing calibration: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing calibration: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("publishing calibration: %w", err)
	}
	return nil
}

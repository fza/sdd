package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

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
type Calibration struct {
	Method        string  `json:"method"`
	LimitTokens   int     `json:"limit_tokens"`
	TokensPerRune float64 `json:"tokens_per_rune"`
	MaxRunes      int     `json:"max_runes"`
}

// Quantise rounds runes down to a multiple of CapQuantum.
func Quantise(runes int) int {
	return runes / CapQuantum * CapQuantum
}

// Calibrate probes emb with single-input requests and derives its rune cap.
// A provider that reports no token count gets FallbackMaxRunes. A provider
// that truncates silently shows a count stuck at its limit; one that rejects
// over-limit input is bisected between the last accepted size and the first
// rejection, so a transport error during bisection reads as a rejection.
func Calibrate(ctx context.Context, emb pkgembed.Embedder) (Calibration, error) {
	latinTokens, err := probe(ctx, emb, repeatRunes("word ", latinProbeRunes))
	if err != nil {
		return Calibration{}, fmt.Errorf("calibration latin probe: %w", err)
	}
	if latinTokens == 0 {
		return Calibration{Method: "unreported", MaxRunes: FallbackMaxRunes}, nil
	}
	cjkTokens, err := probe(ctx, emb, repeatRunes("字", cjkProbeRunes))
	if err != nil {
		return Calibration{}, fmt.Errorf("calibration cjk probe: %w", err)
	}
	worst := math.Max(float64(latinTokens)/latinProbeRunes, float64(cjkTokens)/cjkProbeRunes)

	method, limit, err := findLimit(ctx, emb, latinTokens, worst)
	if err != nil {
		return Calibration{}, err
	}
	maxRunes := Quantise(int(math.Floor(float64(limit) * safetyMargin / worst)))
	if maxRunes < CapQuantum {
		return Calibration{}, fmt.Errorf("calibrated limit of %d tokens leaves no room for a %d-rune cap", limit, CapQuantum)
	}
	return Calibration{Method: method, LimitTokens: limit, TokensPerRune: worst, MaxRunes: maxRunes}, nil
}

func findLimit(ctx context.Context, emb pkgembed.Embedder, latinTokens int, worst float64) (string, int, error) {
	overflowTokens, err := probe(ctx, emb, repeatRunes("word ", overflowProbeRunes))
	if err == nil {
		expected := float64(overflowProbeRunes) * float64(latinTokens) / latinProbeRunes
		if float64(overflowTokens) < plateauFraction*expected {
			return "plateau", overflowTokens, nil
		}
		return "unbounded", overflowTokens, nil
	}
	lo, loTokens, hi := latinProbeRunes, latinTokens, overflowProbeRunes
	for hi-lo > bisectTolerance {
		mid := (lo + hi) / 2
		tokens, err := probe(ctx, emb, repeatRunes("word ", mid))
		if err != nil {
			hi = mid
			continue
		}
		lo, loTokens = mid, tokens
	}
	return "bisect", loTokens, nil
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
// emb and storing the result when none exists. key identifies the serving
// endpoint (fingerprint, URL and path), so the vector store is never touched.
func LoadOrCalibrate(ctx context.Context, emb pkgembed.Embedder, dir, key string) (Calibration, error) {
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(dir, "calibration-"+hex.EncodeToString(sum[:])[:16]+".json")
	data, err := os.ReadFile(path)
	if err == nil {
		var cal Calibration
		if err := json.Unmarshal(data, &cal); err != nil {
			return Calibration{}, fmt.Errorf("reading calibration %s: %w", path, err)
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

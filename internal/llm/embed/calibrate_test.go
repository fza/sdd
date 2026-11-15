package embed_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	localembed "github.com/networkteam/sdd/internal/llm/embed"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/embed"
)

// countingEmbedder reports tokens from tokensFor, and counts requests.
type countingEmbedder struct {
	tokensFor func(text string) (int, error)
	calls     int
}

func (e *countingEmbedder) Embed(_ context.Context, req embed.Request) (embed.Result, error) {
	e.calls++
	tokens, err := e.tokensFor(req.Texts[0])
	if err != nil {
		return embed.Result{}, err
	}
	return embed.Result{Vectors: [][]float32{{0}}, Usage: llm.Usage{InputTokens: tokens}}, nil
}

func (e *countingEmbedder) Fingerprint() string { return "fake" }

// cjkText reports whether text is the CJK probe, which costs one token per rune.
func cjkText(text string) bool { return strings.HasPrefix(text, "字") }

// latinTokens costs a quarter token per rune, as ordinary English does.
func latinTokens(text string) int { return utf8.RuneCountInString(text) / 4 }

func TestCalibrate(t *testing.T) {
	t.Parallel()

	overLimit := errors.New("input exceeds the model limit")
	cases := []struct {
		name       string
		tokensFor  func(text string) (int, error)
		wantMethod string
		wantMax    int
	}{
		{
			name: "silent plateau at the limit",
			tokensFor: func(text string) (int, error) {
				if cjkText(text) {
					return utf8.RuneCountInString(text), nil
				}
				return min(latinTokens(text), 2048), nil
			},
			wantMethod: "plateau",
			wantMax:    1792,
		},
		{
			name: "rejected over the limit is bisected",
			tokensFor: func(text string) (int, error) {
				if cjkText(text) {
					return utf8.RuneCountInString(text), nil
				}
				if latinTokens(text) > 2048 {
					return 0, overLimit
				}
				return latinTokens(text), nil
			},
			wantMethod: "bisect",
			wantMax:    1792,
		},
		{
			name: "no limit inside the probe",
			tokensFor: func(text string) (int, error) {
				if cjkText(text) {
					return utf8.RuneCountInString(text), nil
				}
				return latinTokens(text), nil
			},
			wantMethod: "unbounded",
			wantMax:    4480,
		},
		{
			name: "provider reports no count",
			tokensFor: func(string) (int, error) {
				return 0, nil
			},
			wantMethod: "unreported",
			wantMax:    localembed.FallbackMaxRunes,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cal, err := localembed.Calibrate(context.Background(), &countingEmbedder{tokensFor: tc.tokensFor})
			if err != nil {
				t.Fatalf("Calibrate: %v", err)
			}
			if cal.Method != tc.wantMethod {
				t.Errorf("method: got %q, want %q", cal.Method, tc.wantMethod)
			}
			if cal.MaxRunes != tc.wantMax {
				t.Errorf("max runes: got %d, want %d", cal.MaxRunes, tc.wantMax)
			}
		})
	}
}

func TestQuantise(t *testing.T) {
	t.Parallel()

	cases := []struct {
		runes, want int
	}{
		{runes: 0, want: 0},
		{runes: 63, want: 0},
		{runes: 64, want: 64},
		{runes: 130, want: 128},
		{runes: 1843, want: 1792},
	}
	for _, tc := range cases {
		if got := localembed.Quantise(tc.runes); got != tc.want {
			t.Errorf("Quantise(%d): got %d, want %d", tc.runes, got, tc.want)
		}
	}
}

func TestLoadOrCalibrate_StoredOncePerKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	emb := &countingEmbedder{tokensFor: func(string) (int, error) { return 0, nil }}

	first, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, "fp|http://a|/v1/embeddings")
	if err != nil {
		t.Fatalf("first LoadOrCalibrate: %v", err)
	}
	probes := emb.calls
	second, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, "fp|http://a|/v1/embeddings")
	if err != nil {
		t.Fatalf("second LoadOrCalibrate: %v", err)
	}
	if emb.calls != probes {
		t.Errorf("probes on stored key: got %d extra, want 0", emb.calls-probes)
	}
	if second != first {
		t.Errorf("stored calibration: got %+v, want %+v", second, first)
	}

	other, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, "fp|http://b|/v1/embeddings")
	if err != nil {
		t.Fatalf("other-endpoint LoadOrCalibrate: %v", err)
	}
	if emb.calls == probes {
		t.Errorf("a different endpoint should probe again")
	}
	if other.Method == "" {
		t.Errorf("other-endpoint calibration should carry a method")
	}
}

func TestLoadOrCalibrate_MalformedFailsLoud(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	emb := &countingEmbedder{tokensFor: func(string) (int, error) { return 0, nil }}
	key := "fp|http://c|/v1/embeddings"
	if _, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, key); err != nil {
		t.Fatalf("seed LoadOrCalibrate: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "calibration-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("calibration files: got %d (err %v), want 1", len(files), err)
	}
	if err := os.WriteFile(files[0], []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, key); err == nil {
		t.Error("a malformed stored calibration should be an error")
	}
}

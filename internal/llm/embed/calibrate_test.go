package embed_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
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

func TestPruneCalibrations(t *testing.T) {
	t.Parallel()
	const retention = 90 * 24 * time.Hour
	dir := t.TempDir()
	emb := &countingEmbedder{tokensFor: func(string) (int, error) { return 0, nil }}
	for _, key := range []string{"fp|http://used|/v1/embeddings", "fp|http://unused|/v1/embeddings"} {
		if _, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, key); err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "calibration-*.json"))
	if err != nil || len(files) != 2 {
		t.Fatalf("two calibrations should be stored, got %v, %v", files, err)
	}
	old := time.Now().Add(-retention - time.Hour)
	for _, path := range files {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, "fp|http://used|/v1/embeddings"); err != nil {
		t.Fatal(err)
	}

	removed, err := localembed.PruneCalibrations(dir, time.Now(), retention)
	if err != nil || removed != 1 {
		t.Fatalf("only the calibration no load used within the retention should go, removed %d: %v", removed, err)
	}
	probes := emb.calls
	if _, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, "fp|http://used|/v1/embeddings"); err != nil {
		t.Fatal(err)
	}
	if emb.calls != probes {
		t.Error("the calibration a load used should stay")
	}
}

func TestLoadOrCalibrateRefreshesUseOnceADay(t *testing.T) {
	t.Parallel()
	now := time.Now()
	dir := t.TempDir()
	emb := &countingEmbedder{tokensFor: func(string) (int, error) { return 0, nil }}
	const key = "fp|http://used|/v1/embeddings"
	if _, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, key); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "calibration-*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("one calibration should be stored, got %v, %v", files, err)
	}
	stray := filepath.Join(dir, "notes.json")
	if err := os.WriteFile(stray, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		lastUse   time.Time
		refreshed bool
	}{{now.Add(-time.Hour), false}, {now.Add(-25 * time.Hour), true}} {
		if err := os.Chtimes(files[0], tc.lastUse, tc.lastUse); err != nil {
			t.Fatal(err)
		}
		if _, err := localembed.LoadOrCalibrate(context.Background(), emb, dir, key); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(files[0])
		if err != nil {
			t.Fatal(err)
		}
		if refreshed := info.ModTime().After(tc.lastUse.Add(time.Minute)); refreshed != tc.refreshed {
			t.Errorf("a calibration last used at %v refreshed should be %v, it is %v", tc.lastUse, tc.refreshed, info.ModTime())
		}
	}
	old := now.Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(stray, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := localembed.PruneCalibrations(dir, now, 90*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("pruning should leave a file that is no calibration: %v", err)
	}
}

// limitedEndpoint answers like an endpoint with a 2048-token limit that fails
// over-limit input with overLimit, costing one token per CJK rune.
func limitedEndpoint(overLimit error) func(text string) (int, error) {
	return func(text string) (int, error) {
		if cjkText(text) {
			return utf8.RuneCountInString(text), nil
		}
		if latinTokens(text) > 2048 {
			return 0, overLimit
		}
		return latinTokens(text), nil
	}
}

func droppedConnection() error {
	return &url.Error{Op: "Post", URL: "http://endpoint.test/v1/embeddings", Err: syscall.ECONNRESET}
}

func timedOutRequest() error {
	return &url.Error{Op: "Post", URL: "http://endpoint.test/v1/embeddings", Err: os.ErrDeadlineExceeded}
}

func TestCalibrateRetriesAProbeANetworkErrorEnds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		tokensFor func(text string) (int, error)
	}{
		{
			name: "a connection dropped once inside the limit",
			tokensFor: func() func(string) (int, error) {
				dropped := false
				limited := limitedEndpoint(errors.New("input exceeds the model limit"))
				return func(text string) (int, error) {
					if !dropped && !cjkText(text) && utf8.RuneCountInString(text) > 1000 && latinTokens(text) <= 2048 {
						dropped = true
						return 0, droppedConnection()
					}
					return limited(text)
				}
			}(),
		},
		{
			name: "a request timed out once inside the limit",
			tokensFor: func() func(string) (int, error) {
				timedOut := false
				limited := limitedEndpoint(errors.New("input exceeds the model limit"))
				return func(text string) (int, error) {
					if !timedOut && !cjkText(text) && utf8.RuneCountInString(text) > 1000 && latinTokens(text) <= 2048 {
						timedOut = true
						return 0, timedOutRequest()
					}
					return limited(text)
				}
			}(),
		},
		{
			name:      "an endpoint that drops the connection on over-limit input",
			tokensFor: limitedEndpoint(droppedConnection()),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cal, err := localembed.Calibrate(context.Background(), &countingEmbedder{tokensFor: tc.tokensFor})
			if err != nil {
				t.Fatalf("Calibrate: %v", err)
			}
			if cal.Method != "bisect" || cal.MaxRunes != 1792 {
				t.Errorf("a passing network error should leave the cap the endpoint's limit gives, got %q with %d runes", cal.Method, cal.MaxRunes)
			}
		})
	}
}

func TestLoadOrCalibrateStoresNothingWhenAProbeStaysUnanswered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	unanswered := limitedEndpoint(timedOutRequest())
	if _, err := localembed.LoadOrCalibrate(context.Background(), &countingEmbedder{tokensFor: unanswered}, dir, "fp|http://endpoint.test|/v1/embeddings"); err == nil {
		t.Fatal("a probe that times out twice leaves the limit open and should fail the calibration")
	}
	if files, err := filepath.Glob(filepath.Join(dir, "calibration-*.json")); err != nil || len(files) != 0 {
		t.Fatalf("a failed calibration should store nothing, found %v, %v", files, err)
	}
	next := &countingEmbedder{tokensFor: limitedEndpoint(errors.New("input exceeds the model limit"))}
	cal, err := localembed.LoadOrCalibrate(context.Background(), next, dir, "fp|http://endpoint.test|/v1/embeddings")
	if err != nil {
		t.Fatalf("LoadOrCalibrate: %v", err)
	}
	if next.calls == 0 || cal.MaxRunes != 1792 {
		t.Errorf("the next fill should probe again and store the limit it finds, %d probes, cap %d", next.calls, cal.MaxRunes)
	}
}

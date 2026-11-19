package proctest_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
)

// stockEventTypes are the events a stock build's replay applies; it refuses
// any other as unknown and skips the session.
var stockEventTypes = map[string]bool{
	"session_meta": true, "labeled": true, "started": true, "report": true, "chooser_answer": true,
	"op_result": true, "mutation_intent": true, "mutation_outcome": true, "served": true,
	"transition": true, "completed": true, "abandoned": true, "read": true, "parked": true,
	"served_blocks": true, "reoriented": true,
}

// stockReplay is the stock decode of a session log, cut to what decides
// whether stock can read it: the store's envelope lines, the workflow events
// they carry, and replay's check of each event's version and type.
func stockReplay(log []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(log))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNumber := uint64(0)
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		lineNumber++
		var line struct {
			Version uint64 `json:"version"`
			Events  []struct {
				CodecVersion uint32
				Code         string
				Payload      json.RawMessage
			} `json:"events"`
		}
		if err := json.Unmarshal(raw, &line); err != nil {
			return fmt.Errorf("decoding line %d: %w", lineNumber, err)
		}
		if line.Version != lineNumber {
			return fmt.Errorf("non-sequential version %d at line %d", line.Version, lineNumber)
		}
		for _, stored := range line.Events {
			if stored.Code != "workflow_event" {
				continue
			}
			if stored.CodecVersion != 1 {
				return fmt.Errorf("unsupported workflow event codec %d", stored.CodecVersion)
			}
			var event struct {
				V     int    `json:"v"`
				Seq   int    `json:"seq"`
				Event string `json:"event"`
			}
			if err := json.Unmarshal(stored.Payload, &event); err != nil {
				return fmt.Errorf("decoding workflow event: %w", err)
			}
			if event.V != engine.LogVersion {
				return fmt.Errorf("session log event seq %d has version %d", event.Seq, event.V)
			}
			if !stockEventTypes[event.Event] {
				return fmt.Errorf("replaying seq %d (%s): unknown event type %q", event.Seq, event.Event, event.Event)
			}
		}
	}
	return scanner.Err()
}

// A stock build reading a log this build wrote fails it on the build mark, the
// first event after the header, and so skips the session.
func TestStockRefusesAForkSessionLogOnItsBuildMark(t *testing.T) {
	world := stockCaptureWorld(t)
	session := world.Open(t, "fork-client")
	session.Start(t, "capture", nil)

	log, err := os.ReadFile(filepath.Join(world.SessionsDir, string(session.ID)+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	err = stockReplay(log)
	want := `replaying seq 2 (build_mark): unknown event type "build_mark"`
	if err == nil || err.Error() != want {
		t.Fatalf("stock should refuse the log on the build mark, want %q, got %v", want, err)
	}
}

// The stock decode reads the stock-written fixture whole, so the refusal
// above is the mark's and not the port's.
func TestStockReadsTheStockSessionFixture(t *testing.T) {
	log, err := os.ReadFile(filepath.Join("testdata", "stock_capture_session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := stockReplay(log); err != nil {
		t.Fatalf("the stock decode should read a stock-written log, got %v", err)
	}
}

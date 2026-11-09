package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

func validEvent() sdd.Event {
	return sdd.Event{
		ID: "e1", Channel: "recheck", Type: "sdd.recheck", Time: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC), Writer: "sdd",
		Subject: sdd.EventSubject{URI: "https://tracker.example/issues/7", Entries: []string{"20261109-004506-d-cpt-udz"}},
	}
}

func TestValidateEvent(t *testing.T) {
	err := sdd.ValidateEvent(validEvent())
	if err != nil {
		t.Fatalf("a complete envelope was refused: %v", err)
	}
	cases := map[string]func(*sdd.Event){
		"no ID":                func(e *sdd.Event) { e.ID = "" },
		"ID with a slash":      func(e *sdd.Event) { e.ID = "a/b" },
		"ID too long":          func(e *sdd.Event) { e.ID = strings.Repeat("a", 129) },
		"invalid channel":      func(e *sdd.Event) { e.Channel = "../wip" },
		"type without name":    func(e *sdd.Event) { e.Type = "recheck" },
		"no time":              func(e *sdd.Event) { e.Time = time.Time{} },
		"time out of range":    func(e *sdd.Event) { e.Time = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC) },
		"no writer":            func(e *sdd.Event) { e.Writer = " " },
		"writer not UTF-8":     func(e *sdd.Event) { e.Writer = "\xff" },
		"no subject":           func(e *sdd.Event) { e.Subject = sdd.EventSubject{} },
		"relative subject URI": func(e *sdd.Event) { e.Subject.URI = "issues/7" },
		"empty entry ID":       func(e *sdd.Event) { e.Subject.Entries = []string{""} },
		"entry ID with space":  func(e *sdd.Event) { e.Subject.Entries = []string{"a b"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			event := validEvent()
			mutate(&event)
			err := sdd.ValidateEvent(event)
			if err == nil {
				t.Fatal("an incomplete envelope was accepted")
			}
		})
	}
}

func TestSelectEvents_KeepsTheFirstCopyOfAnID(t *testing.T) {
	first := validEvent()
	copied := validEvent()
	copied.Payload = []byte("later copy")
	other := validEvent()
	other.ID = "e0"
	selected := sdd.SelectEvents([]sdd.Event{first, copied, other}, sdd.EventQuery{})
	if len(selected) != 2 || selected[0].ID != "e0" || selected[1].Payload != nil {
		t.Fatalf("SelectEvents = %+v; want one copy per ID, ordered by time then ID", selected)
	}
}

func newEventApplication(t *testing.T, events sdd.EventStore) *sdd.Application {
	t.Helper()
	graph, err := localadapter.NewFilesystemGraphStore(localadapter.FilesystemGraphStoreOptions{Project: "example", GraphDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := sdd.NewProjectRuntime(sdd.ProjectRuntimeOptions{
		Project: sdd.ProjectRef{ID: "example"}, Graph: graph, Events: events,
		LLM: pkgllm.RunnerFunc(func(context.Context, pkgllm.Request) (pkgllm.Result, error) {
			return pkgllm.Result{}, errors.New("no language model in event tests")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := localadapter.NewFilesystemSessionStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := localadapter.NewFilesystemStagedBlobStoreAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := sdd.NewApplication(sdd.ApplicationOptions{
		Access: &runtimeAccessResolver{runtime: runtime}, Sessions: sessions, StagedBlobs: blobs,
		Clock: sdd.ClockFunc(func() time.Time { return time.Date(2026, 11, 9, 13, 0, 0, 0, time.FixedZone("x", 3600)) }),
	})
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func TestAppendEvents_StampsTheEnvelope(t *testing.T) {
	store := localadapter.NewMemoryEventStore()
	application := newEventApplication(t, store)
	identity := sdd.RequestIdentity{Subject: "local"}
	appended, err := application.AppendEvents(t.Context(), identity, "example", sdd.AppendEventsRequest{
		Channel: "outside",
		Events:  []sdd.EventDraft{{Type: "tracker.comment", Subject: sdd.EventSubject{URI: "https://tracker.example/issues/7"}, Payload: []byte(`{"body":"hi"}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(appended) != 1 || appended[0].ID == "" || appended[0].Writer != "Christopher" || appended[0].Channel != "outside" ||
		!appended[0].Time.Equal(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)) || appended[0].Time.Location() != time.UTC {
		t.Fatalf("AppendEvents = %+v; want an ID, the participant as writer and the clock's time in UTC", appended)
	}
	stored, err := application.QueryEvents(t.Context(), identity, "example", sdd.EventQuery{Types: []string{"tracker.comment"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != appended[0].ID || !bytes.Equal(stored[0].Payload, []byte(`{"body":"hi"}`)) {
		t.Fatalf("QueryEvents = %+v; want the appended event", stored)
	}
}

func TestAppendEvents_RefusesTheReservedNamespace(t *testing.T) {
	store := localadapter.NewMemoryEventStore()
	application := newEventApplication(t, store)
	_, err := application.AppendEvents(t.Context(), sdd.RequestIdentity{Subject: "local"}, "example", sdd.AppendEventsRequest{
		Channel: "outside",
		Events: []sdd.EventDraft{
			{Type: "tracker.comment", Subject: sdd.EventSubject{URI: "https://tracker.example/issues/7"}},
			{Type: "sdd.recheck", Subject: sdd.EventSubject{Entries: []string{"20261109-004506-d-cpt-udz"}}},
		},
	})
	var coded *sdd.ApplicationError
	if !errors.As(err, &coded) || coded.Code != sdd.ErrorInvalidArgument || !strings.Contains(coded.Message, "reserved") {
		t.Fatalf("AppendEvents with an sdd. type = %v; want an invalid-argument refusal naming the reserved namespace", err)
	}
	stored, err := store.Query(t.Context(), sdd.EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("a refused request stored %d events", len(stored))
	}
}

func TestAppendEvents_RefusesAnIncompleteDraft(t *testing.T) {
	application := newEventApplication(t, localadapter.NewMemoryEventStore())
	_, err := application.AppendEvents(t.Context(), sdd.RequestIdentity{Subject: "local"}, "example", sdd.AppendEventsRequest{
		Channel: "outside", Events: []sdd.EventDraft{{Type: "tracker.comment"}},
	})
	var coded *sdd.ApplicationError
	if !errors.As(err, &coded) || coded.Code != sdd.ErrorInvalidArgument {
		t.Fatalf("AppendEvents without a subject = %v; want an invalid-argument refusal", err)
	}
}

func TestEvents_WithoutAStoreFailLoud(t *testing.T) {
	application := newEventApplication(t, nil)
	identity := sdd.RequestIdentity{Subject: "local"}
	_, err := application.QueryEvents(t.Context(), identity, "example", sdd.EventQuery{})
	var coded *sdd.ApplicationError
	if !errors.As(err, &coded) || coded.Code != sdd.ErrorProjectUnavailable {
		t.Fatalf("QueryEvents without a store = %v; want project unavailable", err)
	}
	_, err = application.AppendEvents(t.Context(), identity, "example", sdd.AppendEventsRequest{Channel: "outside"})
	if !errors.As(err, &coded) || coded.Code != sdd.ErrorProjectUnavailable {
		t.Fatalf("AppendEvents without a store = %v; want project unavailable", err)
	}
}

func TestEventsJSON_RoundTripsByteForByte(t *testing.T) {
	source := localadapter.NewMemoryEventStore()
	textPayload := validEvent()
	textPayload.Payload = []byte(`{"body":"<b>ü</b>"}`)
	binaryPayload := validEvent()
	binaryPayload.ID, binaryPayload.Channel, binaryPayload.Type = "e2", "outside", "tracker.blob"
	binaryPayload.Payload = []byte{0x00, 0xff, '{'}
	spacedJSON := validEvent()
	spacedJSON.ID, spacedJSON.Payload = "e3", []byte(`{ "spaced": true }`)
	noPayload := validEvent()
	noPayload.ID, noPayload.Subject = "e4", sdd.EventSubject{Entries: []string{"20261107-115150-d-cpt-0mu"}}
	err := source.Append(t.Context(), []sdd.Event{textPayload, binaryPayload, spacedJSON, noPayload})
	if err != nil {
		t.Fatal(err)
	}
	var exported bytes.Buffer
	err = sdd.ExportEventsJSON(t.Context(), source, sdd.EventQuery{}, &exported)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported.String(), `"payload":{"body":"<b>ü</b>"}`) || !strings.Contains(exported.String(), `"payload_base64":"AP97"`) {
		t.Fatalf("compact JSON payloads should export as themselves, others as base64:\n%s", exported.String())
	}

	target := localadapter.NewMemoryEventStore()
	added, err := sdd.ImportEventsJSON(t.Context(), target, bytes.NewReader(exported.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if added != 4 {
		t.Fatalf("import added %d events into an empty store", added)
	}
	var again bytes.Buffer
	err = sdd.ExportEventsJSON(t.Context(), target, sdd.EventQuery{}, &again)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again.Bytes(), exported.Bytes()) {
		t.Fatalf("export after import differs:\n%s\nwant\n%s", again.String(), exported.String())
	}
	sourceEvents, err := source.Query(t.Context(), sdd.EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	targetEvents, err := target.Query(t.Context(), sdd.EventQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range sourceEvents {
		if !bytes.Equal(targetEvents[i].Payload, sourceEvents[i].Payload) {
			t.Fatalf("event %s payload = %q after the round trip, want the original bytes %q", sourceEvents[i].ID, targetEvents[i].Payload, sourceEvents[i].Payload)
		}
	}
	added, err = sdd.ImportEventsJSON(t.Context(), target, bytes.NewReader(exported.Bytes()))
	if err != nil || added != 0 {
		t.Fatalf("importing stored events again = %d, %v; want nothing added", added, err)
	}
}

func TestImportEventsJSON_RefusesABadLineBeforeAppending(t *testing.T) {
	cases := map[string]string{
		"unknown field":    `{"id":"e9","channel":"recheck","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["x"]},"extra":1}`,
		"both payloads":    `{"id":"e9","channel":"recheck","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["x"]},"payload":{},"payload_base64":"AA=="}`,
		"bad time":         `{"id":"e9","channel":"recheck","type":"sdd.recheck","time":"yesterday","writer":"sdd","subject":{"entries":["x"]}}`,
		"bad base64":       `{"id":"e9","channel":"recheck","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["x"]},"payload_base64":"!"}`,
		"invalid envelope": `{"id":"e9","channel":"recheck","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"","subject":{"entries":["x"]}}`,
		"two values":       `{"id":"e9","channel":"recheck","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["x"]}} {}`,
	}
	good := `{"id":"e1","channel":"recheck","type":"sdd.recheck","time":"2026-11-09T12:00:00Z","writer":"sdd","subject":{"entries":["x"]}}`
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			store := localadapter.NewMemoryEventStore()
			_, err := sdd.ImportEventsJSON(t.Context(), store, strings.NewReader(good+"\n\n"+bad+"\n"))
			if err == nil || !strings.Contains(err.Error(), "line 3") {
				t.Fatalf("ImportEventsJSON = %v; want an error naming the bad line", err)
			}
			stored, err := store.Query(t.Context(), sdd.EventQuery{})
			if err != nil {
				t.Fatal(err)
			}
			if len(stored) != 0 {
				t.Fatalf("a refused import stored %d events", len(stored))
			}
		})
	}
}

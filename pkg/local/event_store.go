package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/segment"
	app "github.com/networkteam/sdd/pkg/application"
)

// eventLog is the layout of a channel's directory: JSON Lines segments, read
// beside the binary ones of internal/segment an earlier sdd wrote, which the
// next compaction rewrites as JSON Lines.
var eventLog = logFiles{write: eventFileExt, read: []string{eventFileExt, segment.Ext}}

const (
	eventFileExt = ".jsonl"
	// eventSegmentKind is the record kind of a binary event segment.
	eventSegmentKind = "sdd.events"
)

// localChannelIgnore keeps a local channel's directory, itself included, out
// of Git, so its segments never reach a commit, sdd's or anyone's. A git
// channel carries no such file, so its new segments are what the next sdd
// auto-commit picks up.
var localChannelIgnore = []byte("# sdd: local event channel, kept out of Git\n*\n")

// EventSettler returns the IDs of the events of channel that compaction
// may drop.
type EventSettler func(ctx context.Context, channel string, events []app.Event) ([]string, error)

// FilesystemEventStoreOptions configures a FilesystemEventStore.
type FilesystemEventStoreOptions struct {
	// Dir holds one directory per channel, normally .sdd/events.
	Dir string
	// GitChannels are the channels shared through Git; every other channel
	// is local.
	GitChannels []string
	// Settled names, from a channel's events, those compaction drops: the
	// expired and healed ones, as each type's writer defines them. Nil drops
	// none.
	Settled EventSettler
}

// FilesystemEventStore is the local default of the project event store: an
// append log per channel of immutable segment files <time>-<random>.jsonl,
// one per Append call and channel, merged by tiered compaction. Each line is
// one event as application.MarshalEventJSON writes it, without the channel
// its directory names. It never commits; see localChannelIgnore for how
// sharing follows the channel.
type FilesystemEventStore struct {
	dir         string
	gitChannels []string
	settled     EventSettler
}

// NewFilesystemEventStore returns the store over options.Dir. Nothing is
// created until the first append.
func NewFilesystemEventStore(options FilesystemEventStoreOptions) (*FilesystemEventStore, error) {
	if options.Dir == "" {
		return nil, fmt.Errorf("sdd: event store directory is required")
	}
	for _, channel := range options.GitChannels {
		err := model.ValidateEventChannel(channel)
		if err != nil {
			return nil, err
		}
	}
	return &FilesystemEventStore{dir: options.Dir, gitChannels: slices.Clone(options.GitChannels), settled: options.Settled}, nil
}

func (s *FilesystemEventStore) Append(ctx context.Context, events []app.Event) error {
	byChannel := map[string][]byte{}
	var channels []string
	for _, event := range events {
		err := app.ValidateEvent(event)
		if err != nil {
			return err
		}
		if _, seen := byChannel[event.Channel]; !seen {
			channels = append(channels, event.Channel)
		}
		byChannel[event.Channel], err = appendEventLine(byChannel[event.Channel], event)
		if err != nil {
			return err
		}
	}
	for _, channel := range channels {
		err := ctx.Err()
		if err != nil {
			return err
		}
		err = s.writeSegment(channel, byChannel[channel])
		if err != nil {
			return fmt.Errorf("event channel %s: %w", channel, err)
		}
		err = s.compactChannel(ctx, channel, false)
		if err != nil {
			return fmt.Errorf("event channel %s: %w", channel, err)
		}
	}
	return nil
}

func (s *FilesystemEventStore) writeSegment(channel string, lines []byte) (err error) {
	dir := filepath.Join(s.dir, channel)
	err = os.MkdirAll(dir, 0o755)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	err = s.markSharing(root, channel)
	if err != nil {
		return err
	}
	return writeLogFile(dir, eventFileExt, lines)
}

// markSharing writes the ignore file into a local channel's directory and
// removes the one sdd wrote from a channel that has turned git.
func (s *FilesystemEventStore) markSharing(root *os.Root, channel string) error {
	current, err := root.ReadFile(".gitignore")
	present := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if slices.Contains(s.gitChannels, channel) {
		if present && bytes.Equal(current, localChannelIgnore) {
			return root.Remove(".gitignore")
		}
		return nil
	}
	if present {
		return nil
	}
	return root.WriteFile(".gitignore", localChannelIgnore, 0o644)
}

func (s *FilesystemEventStore) Query(ctx context.Context, query app.EventQuery) ([]app.Event, error) {
	channels := query.Channels
	if len(channels) == 0 {
		var err error
		channels, err = s.channels()
		if err != nil {
			return nil, err
		}
	}
	var events []app.Event
	for _, channel := range channels {
		err := ctx.Err()
		if err != nil {
			return nil, err
		}
		if model.ValidateEventChannel(channel) != nil {
			continue
		}
		read, err := s.readChannel(ctx, channel)
		if err != nil {
			return nil, fmt.Errorf("event channel %s: %w", channel, err)
		}
		events = append(events, read...)
	}
	return app.SelectEvents(events, query), nil
}

func (s *FilesystemEventStore) channels() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var channels []string
	for _, entry := range entries {
		if entry.IsDir() && model.ValidateEventChannel(entry.Name()) == nil {
			channels = append(channels, entry.Name())
		}
	}
	return channels, nil
}

func (s *FilesystemEventStore) readChannel(ctx context.Context, channel string) ([]app.Event, error) {
	files, err := readLogFiles(ctx, filepath.Join(s.dir, channel), eventLog.read)
	if err != nil {
		return nil, err
	}
	stored, err := decodeEventFiles(channel, files)
	if err != nil {
		return nil, err
	}
	events := make([]app.Event, len(stored))
	for i, event := range stored {
		events[i] = event.Event
	}
	return events, nil
}

// storedEvent is an event with the JSON line that holds it, none for an
// event of a binary segment. Compaction writes the line back as it is, so
// fields only a newer sdd knows survive an older one's compaction
// (20260921-174543-d-cpt-tv9).
type storedEvent struct {
	app.Event
	line []byte
}

func decodeEventFiles(channel string, files []logFile) ([]storedEvent, error) {
	var events []storedEvent
	for _, file := range files {
		decode := decodeEventLines
		if filepath.Ext(file.name) == segment.Ext {
			decode = decodeEventSegment
		}
		read, err := decode(channel, file.data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.path, err)
		}
		events = append(events, read...)
	}
	return events, nil
}

func appendEventLine(lines []byte, event app.Event) ([]byte, error) {
	event.Channel = ""
	line, err := app.MarshalEventJSON(event)
	if err != nil {
		return nil, fmt.Errorf("event %s: %w", event.ID, err)
	}
	return append(lines, line...), nil
}

// decodeEventLines reads a JSON Lines segment. Every line ends in a newline,
// so a last line without one was cut off and the segment reads as corrupt
// instead of as fewer events.
func decodeEventLines(channel string, data []byte) ([]storedEvent, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty file", segment.ErrCorrupt)
	}
	if data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("%w: torn last line, no newline at its end", segment.ErrCorrupt)
	}
	var events []storedEvent
	for number, line := range bytes.Split(data[:len(data)-1], []byte("\n")) {
		event, err := decodeEventLine(channel, line)
		if err != nil {
			return nil, fmt.Errorf("%w: line %d: %w", segment.ErrCorrupt, number+1, err)
		}
		events = append(events, storedEvent{Event: event, line: line})
	}
	return events, nil
}

func decodeEventLine(channel string, line []byte) (app.Event, error) {
	event, err := app.UnmarshalEventJSON(line)
	if err != nil {
		return app.Event{}, err
	}
	if event.Channel != "" && event.Channel != channel {
		return app.Event{}, fmt.Errorf("event %s names channel %s", event.ID, event.Channel)
	}
	event.Channel = channel
	err = app.ValidateEvent(event)
	if err != nil {
		return app.Event{}, err
	}
	return event, nil
}

// decodeEventSegment reads a binary segment an earlier sdd wrote.
func decodeEventSegment(channel string, data []byte) ([]storedEvent, error) {
	records, err := segment.Decode(data, eventSegmentKind)
	if err != nil {
		return nil, err
	}
	events := make([]storedEvent, 0, len(records))
	for _, record := range records {
		event, err := decodeEvent(channel, record)
		if err != nil {
			return nil, err
		}
		events = append(events, storedEvent{Event: event})
	}
	return events, nil
}

// Compact merges every segment of each channel into one, holding each event
// once, without the events the store's Settled hook names.
func (s *FilesystemEventStore) Compact(ctx context.Context) error {
	channels, err := s.channels()
	if err != nil {
		return err
	}
	for _, channel := range channels {
		err := s.compactChannel(ctx, channel, true)
		if err != nil {
			return fmt.Errorf("event channel %s: %w", channel, err)
		}
	}
	return nil
}

// compactChannel merges the channel's segments: every one when full, else tier
// by tier. The Settled hook sees the whole channel once, before the first
// merge; events appended later are never named by it, so they survive.
func (s *FilesystemEventStore) compactChannel(ctx context.Context, channel string, full bool) error {
	var settled map[string]bool
	merge := func(files []logFile) ([]byte, error) {
		if settled == nil {
			var err error
			settled, err = s.settledEvents(ctx, channel)
			if err != nil {
				return nil, err
			}
		}
		events, err := decodeEventFiles(channel, files)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var kept []storedEvent
		for _, event := range events {
			if seen[event.ID] || settled[event.ID] {
				continue
			}
			seen[event.ID] = true
			kept = append(kept, event)
		}
		slices.SortStableFunc(kept, func(a, b storedEvent) int { return a.Time.Compare(b.Time) })
		var lines []byte
		for _, event := range kept {
			if event.line != nil {
				lines = append(append(lines, event.line...), '\n')
				continue
			}
			lines, err = appendEventLine(lines, event.Event)
			if err != nil {
				return nil, err
			}
		}
		return lines, nil
	}
	return compactLog(ctx, filepath.Join(s.dir, channel), eventLog, full, merge)
}

func (s *FilesystemEventStore) settledEvents(ctx context.Context, channel string) (map[string]bool, error) {
	settled := map[string]bool{}
	if s.settled == nil {
		return settled, nil
	}
	events, err := s.readChannel(ctx, channel)
	if err != nil {
		return nil, err
	}
	ids, err := s.settled(ctx, channel, events)
	if err != nil {
		return nil, fmt.Errorf("settled events: %w", err)
	}
	for _, id := range ids {
		settled[id] = true
	}
	return settled, nil
}

func decodeEvent(channel string, record []byte) (app.Event, error) {
	reader := segment.NewReader(record)
	event := app.Event{Channel: channel}
	var err error
	event.ID, err = reader.String()
	if err == nil {
		event.Type, err = reader.String()
	}
	var nanos int64
	if err == nil {
		nanos, err = reader.Varint()
		event.Time = time.Unix(0, nanos).UTC()
	}
	if err == nil {
		event.Writer, err = reader.String()
	}
	if err == nil {
		event.Subject.URI, err = reader.String()
	}
	var count uint64
	if err == nil {
		count, err = reader.Uvarint()
	}
	for i := uint64(0); err == nil && i < count; i++ {
		var entry string
		entry, err = reader.String()
		event.Subject.Entries = append(event.Subject.Entries, entry)
	}
	var payload []byte
	if err == nil {
		payload, err = reader.Bytes()
	}
	if err == nil && !reader.Empty() {
		err = fmt.Errorf("%w: bytes after the event record", segment.ErrCorrupt)
	}
	if err != nil {
		return app.Event{}, err
	}
	if len(payload) > 0 {
		event.Payload = slices.Clone(payload)
	}
	err = app.ValidateEvent(event)
	if err != nil {
		return app.Event{}, fmt.Errorf("%w: %w", segment.ErrCorrupt, err)
	}
	return event, nil
}

// MemoryEventStore is a process-local event store for tests and embedded
// compositions; it holds one copy per event ID.
type MemoryEventStore struct {
	mu     sync.Mutex
	events []app.Event
	known  map[string]bool
}

// NewMemoryEventStore returns an empty in-memory event store.
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{known: map[string]bool{}}
}

func (s *MemoryEventStore) Append(_ context.Context, events []app.Event) error {
	for _, event := range events {
		err := app.ValidateEvent(event)
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range events {
		if s.known[event.ID] {
			continue
		}
		s.known[event.ID] = true
		s.events = append(s.events, cloneEvent(event))
	}
	return nil
}

func (s *MemoryEventStore) Query(_ context.Context, query app.EventQuery) ([]app.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	selected := app.SelectEvents(s.events, query)
	for i := range selected {
		selected[i] = cloneEvent(selected[i])
	}
	return selected, nil
}

func cloneEvent(event app.Event) app.Event {
	event.Time = event.Time.UTC()
	event.Subject.Entries = slices.Clone(event.Subject.Entries)
	event.Payload = slices.Clone(event.Payload)
	if len(event.Payload) == 0 {
		event.Payload = nil
	}
	return event
}

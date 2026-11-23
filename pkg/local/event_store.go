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

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/segment"
	app "github.com/networkteam/sdd/pkg/application"
)

const eventFileExt = ".jsonl"

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
// append log per channel of immutable JSON Lines segments
// <time>-<random>.jsonl, one per Append call and channel, merged by tiered
// compaction. Each line is one event as application.MarshalEventJSON writes
// it, without the channel its directory names. It never commits; see
// localChannelIgnore for how sharing follows the channel.
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
	return writeSegmentFile(dir, eventFileExt, lines)
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
	files, err := readSegmentFiles(ctx, filepath.Join(s.dir, channel), eventFileExt)
	if err != nil {
		return nil, err
	}
	var events []app.Event
	for _, file := range files {
		read, err := decodeEventLines(channel, file.data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file.path, err)
		}
		for _, event := range read {
			events = append(events, event.Event)
		}
	}
	return events, nil
}

// storedEvent is an event with the JSON line that holds it. Compaction writes
// the line back as it is, so fields only a newer sdd knows survive an older
// one's compaction (20260921-174543-d-cpt-tv9).
type storedEvent struct {
	app.Event
	line []byte
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
	merge := func(segments [][]byte) ([]byte, error) {
		if settled == nil {
			var err error
			settled, err = s.settledEvents(ctx, channel)
			if err != nil {
				return nil, err
			}
		}
		seen := map[string]bool{}
		var kept []storedEvent
		for _, data := range segments {
			events, err := decodeEventLines(channel, data)
			if err != nil {
				return nil, err
			}
			for _, event := range events {
				if seen[event.ID] || settled[event.ID] {
					continue
				}
				seen[event.ID] = true
				kept = append(kept, event)
			}
		}
		slices.SortStableFunc(kept, func(a, b storedEvent) int { return a.Time.Compare(b.Time) })
		var lines []byte
		for _, event := range kept {
			lines = append(append(lines, event.line...), '\n')
		}
		return lines, nil
	}
	return compactDir(ctx, filepath.Join(s.dir, channel), eventFileExt, full, merge)
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

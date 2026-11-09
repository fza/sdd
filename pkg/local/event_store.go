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

const eventSegmentKind = "sdd.events"

// localChannelIgnore keeps a local channel's directory, itself included, out
// of Git, so its segments never reach a commit, sdd's or anyone's. A git
// channel carries no such file, so its new segments are what the next sdd
// auto-commit picks up (20261109-004506-d-cpt-udz, D19).
var localChannelIgnore = []byte("# sdd: local event channel, kept out of Git\n*\n")

// FilesystemEventStoreOptions configures a FilesystemEventStore.
type FilesystemEventStoreOptions struct {
	// Dir holds one directory per channel, normally .sdd/events.
	Dir string
	// GitChannels are the channels shared through Git; every other channel
	// is local.
	GitChannels []string
}

// FilesystemEventStore is the local default of the project event store: an
// append log per channel of immutable segment files <time>-<random>.seg, one
// per Append call and channel (20261109-004506-d-cpt-udz, D15 and D17). It
// never commits; see localChannelIgnore for how sharing follows the channel.
type FilesystemEventStore struct {
	dir         string
	gitChannels []string
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
	return &FilesystemEventStore{dir: options.Dir, gitChannels: slices.Clone(options.GitChannels)}, nil
}

func (s *FilesystemEventStore) Append(ctx context.Context, events []app.Event) error {
	byChannel := map[string][][]byte{}
	var channels []string
	for _, event := range events {
		err := app.ValidateEvent(event)
		if err != nil {
			return err
		}
		if _, seen := byChannel[event.Channel]; !seen {
			channels = append(channels, event.Channel)
		}
		byChannel[event.Channel] = append(byChannel[event.Channel], encodeEvent(event))
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
	}
	return nil
}

func (s *FilesystemEventStore) writeSegment(channel string, records [][]byte) (err error) {
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
	name, err := segment.Name(time.Now())
	if err != nil {
		return err
	}
	temp := "." + name + ".tmp"
	file, err := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = file.Write(segment.Encode(eventSegmentKind, records))
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = root.Rename(temp, name)
	}
	if err != nil {
		return errors.Join(err, root.Remove(temp))
	}
	return nil
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
		read, err := s.readChannel(channel)
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

func (s *FilesystemEventStore) readChannel(channel string) ([]app.Event, error) {
	dir := filepath.Join(s.dir, channel)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var events []app.Event
	for _, entry := range entries {
		if entry.IsDir() || !segment.IsName(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		records, err := segment.Decode(data, eventSegmentKind)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, record := range records {
			event, err := decodeEvent(channel, record)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			events = append(events, event)
		}
	}
	return events, nil
}

func encodeEvent(event app.Event) []byte {
	record := segment.AppendString(nil, event.ID)
	record = segment.AppendString(record, event.Type)
	record = segment.AppendVarint(record, event.Time.UnixNano())
	record = segment.AppendString(record, event.Writer)
	record = segment.AppendString(record, event.Subject.URI)
	record = segment.AppendUvarint(record, uint64(len(event.Subject.Entries)))
	for _, entry := range event.Subject.Entries {
		record = segment.AppendString(record, entry)
	}
	return segment.AppendBytes(record, event.Payload)
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

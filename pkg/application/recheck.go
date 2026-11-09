package application

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

// HandEdit is a commit outside sdd's own that changed a committed entry
// (20261109-004506-d-cpt-udz, D5).
type HandEdit struct {
	Commit string
	Entry  string
	Time   time.Time
}

// HandEditScanner lists the hand edits committed after since, read from the
// project's version history.
type HandEditScanner interface {
	HandEdits(ctx context.Context, since time.Time) ([]HandEdit, error)
}

// recheckWriter is the writer the recheck events carry.
const recheckWriter = "sdd"

// recheckItems returns the project's recheck items and the time of its
// hand-edit baseline, zero before the first scan; none without an event
// store.
func (r *ProjectRuntime) recheckItems(ctx context.Context) ([]model.RecheckItem, time.Time, error) {
	if r.options.Events == nil {
		return nil, time.Time{}, nil
	}
	events, err := r.options.Events.Query(ctx, EventQuery{Channels: []string{model.RecheckChannel}, Types: []string{model.RecheckEventType, model.RecheckBaselineType}})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("reading the recheck list: %w", err)
	}
	var items []model.RecheckItem
	var baseline time.Time
	for _, event := range events {
		if event.Type == model.RecheckBaselineType {
			if baseline.IsZero() || event.Time.Before(baseline) {
				baseline = event.Time
			}
			continue
		}
		item, err := model.RecheckItemFromEvent(event.ID, event.Time, event.Subject.Entries, event.Payload)
		if err != nil {
			return nil, time.Time{}, err
		}
		items = append(items, item)
	}
	return items, baseline, nil
}

// appendRecheck stores items as sdd.recheck events; an item stored before
// keeps its one copy.
func (r *ProjectRuntime) appendRecheck(ctx context.Context, items []model.RecheckItem) error {
	if len(items) == 0 {
		return nil
	}
	store, err := r.eventStore()
	if err != nil {
		return err
	}
	events := make([]Event, 0, len(items))
	for _, item := range items {
		payload, err := item.Payload()
		if err != nil {
			return err
		}
		events = append(events, Event{ID: item.ID, Channel: model.RecheckChannel, Type: model.RecheckEventType, Time: item.At,
			Writer: recheckWriter, Subject: EventSubject{Entries: slices.Clone(item.Entries)}, Payload: payload})
	}
	err = store.Append(ctx, events)
	if err != nil {
		return fmt.Errorf("storing recheck items: %w", err)
	}
	return nil
}

// ScanHandEdits records every hand edit committed since the project's
// baseline as a recheck item and returns how many it found. The first scan
// only sets the baseline: edits from before the recheck list began are not
// its work.
func (a *Application) ScanHandEdits(ctx context.Context, identity RequestIdentity, project ProjectID) (int, error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessWrite)
	if err != nil {
		return 0, err
	}
	return runtime.scanHandEdits(ctx, a.clock.Now())
}

func (r *ProjectRuntime) scanHandEdits(ctx context.Context, now time.Time) (int, error) {
	if r.options.HandEdits == nil || r.options.Events == nil {
		return 0, nil
	}
	_, baseline, err := r.recheckItems(ctx)
	if err != nil {
		return 0, err
	}
	if baseline.IsZero() {
		mark := Event{ID: fmt.Sprintf("recheck-baseline-%d", now.UnixNano()), Channel: model.RecheckChannel, Type: model.RecheckBaselineType,
			Time: now.UTC(), Writer: recheckWriter, Subject: EventSubject{URI: "sdd:recheck"}}
		err = r.options.Events.Append(ctx, []Event{mark})
		if err != nil {
			return 0, fmt.Errorf("storing the recheck baseline: %w", err)
		}
		return 0, nil
	}
	edits, err := r.options.HandEdits.HandEdits(ctx, baseline)
	if err != nil {
		return 0, err
	}
	items := make([]model.RecheckItem, 0, len(edits))
	for _, edit := range edits {
		items = append(items, model.HandEditItem(edit.Entry, edit.Commit, edit.Time))
	}
	return len(items), r.appendRecheck(ctx, items)
}

// openRecheck returns the recheck items no later entry in g healed.
func (r *ProjectRuntime) openRecheck(ctx context.Context, g *model.Graph) ([]model.RecheckItem, error) {
	items, _, err := r.recheckItems(ctx)
	if err != nil {
		return nil, err
	}
	return model.UnhealedRecheck(g, items), nil
}

// recheckResponsible reports whether participant runs the project's recheck
// work: everyone while no participant is named (D5).
func recheckResponsible(policy GroundingPolicy, participant string) bool {
	return len(policy.RecheckResponsible) == 0 || slices.Contains(policy.RecheckResponsible, participant)
}

// RecheckList is the rendered recheck list of a project.
type RecheckList struct {
	Open int
	Text string
}

// RecheckList returns the recheck items no entry on the default branch
// healed, rendered with the healing suggestion.
func (a *Application) RecheckList(ctx context.Context, identity RequestIdentity, project ProjectID) (result RecheckList, err error) {
	_, runtime, err := a.resolve(ctx, identity, project, AccessRead)
	if err != nil {
		return RecheckList{}, err
	}
	selected, err := acquireSnapshotForReadBranch(ctx, runtime, "")
	if err != nil {
		return RecheckList{}, err
	}
	defer selected.releaseInto(&err)
	graph := selected.snapshot.graph
	open, err := runtime.openRecheck(ctx, graph)
	if err != nil {
		return RecheckList{}, err
	}
	var rendered bytes.Buffer
	presenters.RenderRecheckItems(&rendered, graph, open)
	return RecheckList{Open: len(open), Text: strings.TrimRight(rendered.String(), "\n")}, nil
}

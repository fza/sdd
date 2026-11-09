package application_test

import (
	"context"
	"strings"
	"testing"
	"time"

	sdd "github.com/networkteam/sdd/pkg/application"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

type scriptedHandEdits struct {
	edits []sdd.HandEdit
	since []time.Time
}

func (s *scriptedHandEdits) HandEdits(_ context.Context, since time.Time) ([]sdd.HandEdit, error) {
	s.since = append(s.since, since)
	var out []sdd.HandEdit
	for _, edit := range s.edits {
		if edit.Time.After(since) {
			out = append(out, edit)
		}
	}
	return out, nil
}

// The first scan sets the baseline; later scans record each hand edit since
// it once, and the list shows it until an entry heals it (D5).
func TestScanHandEdits_RecordsEditsSinceTheBaseline(t *testing.T) {
	f := newGatherFixture(t)
	f.seed(t)
	scanner := &scriptedHandEdits{edits: []sdd.HandEdit{{Commit: "c0ffee", Entry: gatherAlpha, Time: time.Now().Add(-48 * time.Hour)}}}
	f.events = localadapter.NewMemoryEventStore()
	f.handEdits = scanner
	application := f.app(t, sdd.GroundingPolicy{}, nil)

	found, err := application.ScanHandEdits(t.Context(), recheckIdentity, gatherProject)
	if err != nil || found != 0 || len(scanner.since) != 0 {
		t.Fatalf("found %d, err %v: the first scan should only set the baseline", found, err)
	}
	scanner.edits = append(scanner.edits, sdd.HandEdit{Commit: "beef", Entry: gatherAlpha, Time: time.Now().Add(time.Minute)})
	for range 2 {
		found, err = application.ScanHandEdits(t.Context(), recheckIdentity, gatherProject)
		if err != nil || found != 1 {
			t.Fatalf("found %d, err %v: an edit after the baseline should be found", found, err)
		}
	}
	list, err := application.RecheckList(t.Context(), recheckIdentity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if list.Open != 1 || !strings.Contains(list.Text, "hand edit in commit beef") || !strings.Contains(list.Text, gatherAlpha) {
		t.Fatalf("list %+v: the edit found twice should be one item", list)
	}

	f.write(t, "20991231-000000-d-tac-hea", "directive", "Restates the alpha entry after its hand edit.", "intent: pending\nrefs:\n  - id: "+gatherAlpha+"\n    kind: addresses\n")
	list, err = application.RecheckList(t.Context(), recheckIdentity, gatherProject)
	if err != nil {
		t.Fatal(err)
	}
	if list.Open != 0 {
		t.Fatalf("list %+v: a later entry citing the edited entry should heal the item", list)
	}
}

var recheckIdentity = sdd.RequestIdentity{Subject: "tester"}

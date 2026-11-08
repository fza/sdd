package model_test

import (
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestLocationRefRoundTrip(t *testing.T) {
	refs := []model.LocationRef{
		model.GitDirLocation(".sdd/graph"),
		model.GitPathLocation("../graph project?", "graph"),
		model.GitRepoLocation("example.com/org/graph", "graph", "main"),
	}
	for _, ref := range refs {
		parsed, err := model.ParseLocationRef(ref.String())
		if err != nil {
			t.Fatalf("ParseLocationRef(%q): %v", ref.String(), err)
		}
		if parsed.String() != ref.String() || parsed.Address != ref.Address || parsed.Dir() != ref.Dir() {
			t.Errorf("a reference should read back as written, got %+v from %q", parsed, ref.String())
		}
	}
}

func TestParseLocationRefRefuses(t *testing.T) {
	cases := map[string]string{
		"neo4j:bolt://graph":                 "unknown kind neo4j",
		"git-path:../graph":                  "needs the option dir",
		"git-repo:example.com/org/g?dir=x":   "needs the option branch",
		"git-dir:graph?dir=x":                "takes no option dir",
		"git-dir:":                           "needs an address",
		"graph":                              "want <kind>:<address>",
		"git-repo:not a repo?dir=x&branch=m": "",
	}
	for text, want := range cases {
		_, err := model.ParseLocationRef(text)
		if err == nil {
			t.Errorf("ParseLocationRef(%q) should be refused", text)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ParseLocationRef(%q) should say %q, got %v", text, want, err)
		}
	}
}

func TestMoveRecordTrailersRoundTrip(t *testing.T) {
	record := model.MoveRecord{
		ID:       "01JC0000000000000000000000",
		From:     model.GitDirLocation(".sdd/graph"),
		To:       model.GitPathLocation("../graph", "graph"),
		Revision: "abc123",
		Entries:  42,
	}
	parsed, found, err := model.ParseMoveRecord("Signed-off-by: someone\n" + record.Trailers() + "sdd-move-note: later field\n")
	if err != nil || !found {
		t.Fatalf("ParseMoveRecord: found=%v err=%v", found, err)
	}
	if parsed.ID != record.ID || parsed.From.String() != record.From.String() || parsed.To.String() != record.To.String() || parsed.Revision != record.Revision || parsed.Entries != record.Entries {
		t.Errorf("a record should read back as written, got %+v", parsed)
	}

	_, found, err = model.ParseMoveRecord("Signed-off-by: someone\n")
	if found || err != nil {
		t.Errorf("trailers without a record should carry none, got found=%v err=%v", found, err)
	}
	_, found, err = model.ParseMoveRecord(strings.Replace(record.Trailers(), "sdd-move: v1", "sdd-move: v2", 1))
	if !found || err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("an unknown record version should be refused, got found=%v err=%v", found, err)
	}
	_, _, err = model.ParseMoveRecord(strings.Replace(record.Trailers(), "sdd-move-revision: abc123\n", "", 1))
	if err == nil {
		t.Error("a record lacking a field should be refused")
	}
}

func TestFindStrays(t *testing.T) {
	current := model.GitDirLocation("graph")
	records := []model.MoveRecord{
		{From: model.GitDirLocation(".sdd/graph"), To: model.GitPathLocation("../graph", "graph")},
		{From: model.GitPathLocation("../graph", "graph"), To: current},
	}
	former := model.FormerLocations(records, current)
	if len(former) != 2 || former[0].String() != "git-dir:.sdd/graph" || former[1].String() != "git-path:../graph?dir=graph" {
		t.Fatalf("former locations should be every recorded location but the current one, each once, got %v", former)
	}

	held := map[string]bool{"20261108-120000-s-tac-old": true}
	strays := model.FindStrays([]model.FormerFolder{{
		Location: former[0], Dir: "/repo/.sdd/graph",
		Files: []string{"2026/11/08-120000-s-tac-old.md", "2026/11/08-130000-d-cpt-new.md", "2026/11/08-130000-d-cpt-new/notes.md", "README.md"},
	}}, func(id string) bool { return held[id] })
	if len(strays) != 1 || strays[0].ID != "20261108-130000-d-cpt-new" || strays[0].Path != "/repo/.sdd/graph/2026/11/08-130000-d-cpt-new.md" {
		t.Fatalf("only an entry the graph does not hold should be a stray, got %+v", strays)
	}

	err := model.StrayRefusal(strays, current)
	if err == nil || !strings.Contains(err.Error(), strays[0].Path) || !strings.Contains(err.Error(), "sdd move --graph-dir graph") {
		t.Errorf("the refusal should name the files and the move that carries them over, got %v", err)
	}
	if model.StrayRefusal(nil, current) != nil {
		t.Error("no strays should refuse nothing")
	}
}

func TestFormatULID(t *testing.T) {
	at := time.UnixMilli(1469918176385)
	got := model.FormatULID(at, [10]byte{})
	if len(got) != 26 || !strings.HasPrefix(got, "01ARYZ6S41") || strings.Trim(got[10:], "0") != "" {
		t.Errorf("a ULID should encode the millisecond time in its first ten characters, got %q", got)
	}
	later := model.FormatULID(at.Add(time.Millisecond), [10]byte{0xff})
	if later <= got {
		t.Error("a later ULID should sort after an earlier one")
	}
}

func TestMoveMinimumVersion(t *testing.T) {
	cases := []struct {
		name, recorded, binary, want string
	}{
		{name: "dev build, no floor", binary: "dev", want: model.MoveRecordsMinimumVersion},
		{name: "newer release", recorded: "v0.10.0", binary: "v9.0.0", want: model.MoveRecordsMinimumVersion},
		{name: "higher floor kept", recorded: "v9.0.0", binary: "dev", want: "v9.0.0"},
		{name: "older release making the move", recorded: "v0.1.0", binary: "v0.18.0-alpha.2.fza.43", want: "v0.18.0-alpha.2.fza.43"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.MoveMinimumVersion(tc.recorded, tc.binary); got != tc.want {
				t.Errorf("MoveMinimumVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

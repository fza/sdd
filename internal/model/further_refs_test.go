package model_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/model"
)

func TestRefMissRetry(t *testing.T) {
	recorded := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	miss := model.RefMiss{Repo: "example.test/c", EntryID: "20261101-100000-s-tac-aaa", Commit: "abc", At: recorded}
	for _, tt := range []struct {
		name   string
		commit string
		now    time.Time
		want   bool
	}{
		{name: "clone unchanged within a day", commit: "abc", now: recorded.Add(23 * time.Hour), want: false},
		{name: "clone commit moved", commit: "def", now: recorded.Add(time.Minute), want: true},
		{name: "a day passed", commit: "abc", now: recorded.Add(model.RefMissRetry), want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := miss.Retry(tt.commit, tt.now); got != tt.want {
				t.Errorf("retry should be %v when the %s", tt.want, tt.name)
			}
		})
	}
}

func fixtureEntry(id string, refs ...string) *model.Entry {
	parts, err := model.ParseID(id)
	if err != nil {
		panic(err)
	}
	e := &model.Entry{ID: id, Type: model.TypeFromAbbrev[parts.TypeCode], Layer: model.LayerFromAbbrev[parts.LayerCode], Time: parts.Time, Summary: "Summary of " + id}
	for _, ref := range refs {
		e.Refs = append(e.Refs, model.Ref{ID: ref, Kind: model.RefKindRelated})
	}
	return e
}

// furtherCall records one call of the resolver.
type furtherCall struct {
	repo    model.RepoID
	entry   string
	fetched bool
}

const (
	depRepo     model.RepoID = "example.test/b"
	furtherRepo model.RepoID = "example.test/c"
	presentID                = "20261101-100000-s-tac-pre"
	absentID                 = "20261101-110000-s-tac-abs"
	otherID                  = "20261101-120000-s-tac-oth"
)

// newFurtherGraph assembles home A, its direct dependency B and a resolver
// for C, which only B declares; B's entry refs C's entries.
func newFurtherGraph(t *testing.T, answer func(entry string, fetched bool) model.FurtherRef) (*model.Graph, *[]furtherCall) {
	t.Helper()
	local := model.NewGraph([]*model.Entry{fixtureEntry("20261110-100000-d-tac-loc", string(depRepo)+":20261105-100000-d-tac-dep", string(furtherRepo)+":"+presentID)})
	dep := model.NewGraph([]*model.Entry{fixtureEntry("20261105-100000-d-tac-dep",
		string(furtherRepo)+":"+presentID, string(furtherRepo)+":"+absentID, string(furtherRepo)+":"+otherID)})
	multi := model.NewMultiGraph(local, []model.RepoID{depRepo}, func(repoID model.RepoID) (*model.Graph, error) {
		if repoID == depRepo {
			return dep, nil
		}
		return nil, nil
	})
	var calls []furtherCall
	multi.ResolveFurther(func(repoID model.RepoID, entryID string, fetched bool) (model.FurtherRef, error) {
		calls = append(calls, furtherCall{repo: repoID, entry: entryID, fetched: fetched})
		return answer(entryID, fetched), nil
	})
	return local, &calls
}

func furtherClone(ids ...string) *model.Graph {
	var entries []*model.Entry
	for _, id := range ids {
		entries = append(entries, fixtureEntry(id, "example.test/d:20261001-100000-s-tac-ddd"))
	}
	return model.NewGraph(entries)
}

func TestFurtherRefsResolveFromTheClone(t *testing.T) {
	fetched := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	local, calls := newFurtherGraph(t, func(string, bool) model.FurtherRef {
		return model.FurtherRef{Graph: furtherClone(presentID), CloneFetched: fetched}
	})
	dep, err := local.MemberGraph(depRepo)
	if err != nil || dep == nil {
		t.Fatalf("direct dependency should load: %v", err)
	}

	e, owner, ok := dep.ResolveAcross(string(furtherRepo) + ":" + presentID)
	if !ok || e.Summary != "Summary of "+presentID {
		t.Fatalf("a ref the dependency holds should resolve from the clone, got %v", ok)
	}
	if at, further := owner.CloneFetched(); !further || !at.Equal(fetched) {
		t.Errorf("the resolved entry should carry its clone's age, got %v %v", at, further)
	}
	if _, _, ok := owner.ResolveAcross("example.test/d:20261001-100000-s-tac-ddd"); ok || len(*calls) != 1 {
		t.Error("a further-out graph's cross-repo refs should resolve no further")
	}
	if _, _, ok := local.ResolveAcross(string(furtherRepo) + ":" + presentID); ok {
		t.Error("the home's own ref beyond its direct dependencies should stay unresolved")
	}
	if _, _, ok := dep.ResolveAcross(string(furtherRepo) + ":" + presentID); !ok || len(*calls) != 1 {
		t.Errorf("a second lookup should reuse the first resolution, resolver called %d times", len(*calls))
	}
}

func TestFurtherRefsFetchOncePerRead(t *testing.T) {
	at := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	local, calls := newFurtherGraph(t, func(entry string, fetched bool) model.FurtherRef {
		if !fetched {
			return model.FurtherRef{Graph: furtherClone(presentID), Fetched: true, Unavailable: &model.UnavailableRef{Repo: furtherRepo, EntryID: entry, Kind: model.RefMissingAfterFetch, At: at}}
		}
		return model.FurtherRef{Graph: furtherClone(presentID), Unavailable: &model.UnavailableRef{Repo: furtherRepo, EntryID: entry, Kind: model.RefMissingAfterFetch, At: at}}
	})
	dep, _ := local.MemberGraph(depRepo)
	for _, id := range []string{absentID, otherID} {
		if _, _, ok := dep.ResolveAcross(string(furtherRepo) + ":" + id); ok {
			t.Fatalf("%s should stay unresolved", id)
		}
	}
	if len(*calls) != 2 || (*calls)[0].fetched || !(*calls)[1].fetched {
		t.Errorf("the second miss should know the fetch already ran, calls = %+v", *calls)
	}
	if _, _, ok := dep.ResolveAcross(string(furtherRepo) + ":" + presentID); !ok || len(*calls) != 2 {
		t.Errorf("an entry the fetched clone holds should resolve without another call, calls = %+v", *calls)
	}
	u := dep.UnavailableRef(string(furtherRepo) + ":" + absentID)
	if u == nil || u.Kind != model.RefMissingAfterFetch || u.Reason() != "not in example.test/c as of "+at.Local().Format(time.DateTime) {
		t.Errorf("the miss should show why, got %+v", u)
	}
	if got := local.UnavailableRefs(); len(got) != 2 {
		t.Errorf("each unresolved ref should yield one warning, got %+v", got)
	}
}

func TestFurtherRefsBlockedRepoAnswersEveryEntry(t *testing.T) {
	for _, kind := range []model.UnavailableRefKind{model.RefNotConnected, model.RefFetchDeferred} {
		t.Run(string(kind), func(t *testing.T) {
			next := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
			local, calls := newFurtherGraph(t, func(entry string, _ bool) model.FurtherRef {
				return model.FurtherRef{Unavailable: &model.UnavailableRef{Repo: furtherRepo, EntryID: entry, Kind: kind, At: next}}
			})
			dep, _ := local.MemberGraph(depRepo)
			for _, id := range []string{presentID, absentID} {
				if _, _, ok := dep.ResolveAcross(string(furtherRepo) + ":" + id); ok {
					t.Fatalf("%s should stay unresolved", id)
				}
			}
			if len(*calls) != 1 {
				t.Errorf("a blocked repo should be asked once per read, calls = %+v", *calls)
			}
			u := dep.UnavailableRef(string(furtherRepo) + ":" + absentID)
			if u == nil || u.Kind != kind || u.EntryID != absentID || u.Actor() == "" {
				t.Errorf("every entry of the blocked repo should carry its kind and who must act, got %+v", u)
			}
		})
	}
}

package application_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/internal/testsupport"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/llm"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

func fixtureCredential(t *testing.T, rule string) string {
	t.Helper()
	content, err := fs.ReadFile(testsupport.SecretFixtures(), "positive/"+rule+".txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(content))
}

// noModelCall fails the test on any LLM request: a credential must be refused
// before the engine sends the draft to a provider.
func noModelCall(t *testing.T) llm.Runner {
	return llm.RunnerFunc(func(_ context.Context, request llm.Request) (llm.Result, error) {
		t.Errorf("the draft reached the model (%s) despite holding a credential", request.Purpose)
		return llm.Result{}, errors.New("must not be called")
	})
}

func assertCredentialRefusal(t *testing.T, err error, where, credential string) {
	t.Helper()
	if !errors.Is(err, secrets.ErrCredential) {
		t.Fatalf("the write should be refused as holding a credential: %v", err)
	}
	if !strings.Contains(err.Error(), where) {
		t.Errorf("the refusal should name where the credential sits: %v", err)
	}
	if strings.Contains(err.Error(), credential) {
		t.Error("the refusal must never repeat the credential")
	}
}

func TestEngineRefusesACredentialBeforeAnyModelCall(t *testing.T) {
	credential := fixtureCredential(t, "github-pat")
	cases := []struct {
		name  string
		call  func(f *writeFixture) error
		where string
	}{
		{name: "pre-flight", call: func(f *writeFixture) error {
			_, err := f.app.PreflightEntry(t.Context(), f.identity, "example", f.binding, sdd.EntryDraft{Kind: "gap", Layer: "tactical", Confidence: "high", Body: "Setup:\n" + credential})
			return err
		}, where: "the body"},
		{name: "create", call: func(f *writeFixture) error {
			draft := f.identifiedDraft(sdd.EntryDraft{Kind: "gap", Layer: "tactical", Confidence: "high", Body: "Setup:\n" + credential})
			_, err := f.app.CreateEntry(t.Context(), f.identity, "example", f.binding, draft)
			return err
		}, where: "the body"},
		{name: "writing guide", call: func(f *writeFixture) error {
			_, err := f.app.WritingGuideCheck(t.Context(), f.identity, "example", sdd.EntryDraft{Kind: "gap", Layer: "tactical", Body: credential})
			return err
		}, where: "the body"},
		{name: "staged attachment", call: func(f *writeFixture) error {
			_, err := f.app.StageBlob(t.Context(), f.identity, "example", sdd.SessionRef{Subject: f.binding.Subject, Session: f.binding.SessionID}, "env.txt", []byte(credential+"\n"))
			return err
		}, where: "the attachment env.txt"},
		{name: "summary correction", call: func(f *writeFixture) error {
			_, err := f.app.ReplaceSummary(t.Context(), f.identity, "example", f.binding, sdd.SummaryReplacement{Summary: credential})
			return err
		}, where: "the summary"},
		{name: "WIP marker", call: func(f *writeFixture) error {
			_, err := f.app.StartWIP(t.Context(), f.identity, "example", f.binding, sdd.WIPMarkerWrite{Description: credential})
			return err
		}, where: "the WIP description"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWriteFixture(t, writeFixtureOptions{Runner: noModelCall(t)})

			err := tc.call(f)

			assertCredentialRefusal(t, err, tc.where, credential)
			files, globErr := filepath.Glob(filepath.Join(f.graphDir, "*", "*", "*"))
			if globErr != nil || len(files) != 0 {
				t.Errorf("a refused write should leave the graph untouched: %v %v", files, globErr)
			}
		})
	}
}

func TestEngineRefusesAGeneratedSummaryHoldingACredential(t *testing.T) {
	credential := fixtureCredential(t, "gitlab-pat")
	f := newWriteFixture(t, writeFixtureOptions{Runner: llm.RunnerFunc(func(_ context.Context, request llm.Request) (llm.Result, error) {
		return llm.Result{Text: credential}, nil
	})})
	draft := f.identifiedDraft(sdd.EntryDraft{Kind: "gap", Layer: "tactical", Confidence: "high", Body: "A clean body."})

	_, err := f.app.CreateEntry(t.Context(), f.identity, "example", f.binding, draft)

	assertCredentialRefusal(t, err, "the summary", credential)
	files, globErr := filepath.Glob(filepath.Join(f.graphDir, "*", "*", "*.md"))
	if globErr != nil || len(files) != 0 {
		t.Errorf("a refused capture should publish nothing: %v %v", files, globErr)
	}
}

// A credential already stored in the graph reaches the model provider as
// context of a new draft: it blocks neither a pre-flight that pulls the entry
// in nor the capture that supersedes it. sdd lint reports it instead.
func TestEngineSendsAStoredCredentialAsContext(t *testing.T) {
	const storedID = "20260101-120000-s-tac-lek"
	credential := fixtureCredential(t, "github-pat")
	var preflights int
	runner := secrets.GuardRunner(llm.RunnerFunc(func(_ context.Context, request llm.Request) (llm.Result, error) {
		if request.Purpose == llm.PurposePreflight {
			preflights++
			return llm.Result{Text: `{"findings":[]}`}, nil
		}
		return llm.Result{Text: "The leaked deployment notes are replaced."}, nil
	}))
	f := newWriteFixture(t, writeFixtureOptions{Runner: runner})
	stored := filepath.Join(f.graphDir, "2026", "01", "01-120000-s-tac-lek.md")
	err := os.MkdirAll(filepath.Dir(stored), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(stored, []byte("---\ntype: signal\nlayer: tactical\nkind: gap\nconfidence: high\nsummary: Deployment notes.\n---\n\nThe deployment notes held "+credential+".\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	draft := sdd.EntryDraft{Kind: "gap", Layer: "tactical", Confidence: "high", Body: "The deployment notes are replaced without the token.", Supersedes: []string{storedID}}

	_, err = f.app.PreflightEntry(t.Context(), f.identity, "example", f.binding, draft)
	if err != nil {
		t.Fatalf("a pre-flight pulling in the stored entry should run: %v", err)
	}
	if preflights != 1 {
		t.Error("the pre-flight should reach the model")
	}
	created, err := f.app.CreateEntry(t.Context(), f.identity, "example", f.binding, f.identifiedDraft(draft))
	if err != nil {
		t.Fatalf("the entry superseding the stored one should be published: %v", err)
	}
	if got := loadEntryByID(t, f.graphDir, created.EntryID).Supersedes; len(got) != 1 || got[0] != storedID {
		t.Errorf("the published entry should supersede the stored one, supersedes %v", got)
	}
}

// A credential-shaped search phrase never reaches the embedding provider: it is
// searched literally, and the response says so.
func TestSearchKeepsACredentialPhraseOffTheEmbeddingProvider(t *testing.T) {
	embeddings := &countingEmbeddings{}
	runtime := indexRuntime(t, "base", localadapter.NewMemorySearchIndexStore(), embeddings)
	app, err := sdd.NewApplication(sdd.ApplicationOptions{Access: &multiAccessResolver{base: runtime}, Sessions: noSessionStore{}, StagedBlobs: noBlobStore{}})
	if err != nil {
		t.Fatal(err)
	}
	credential := fixtureCredential(t, "github-pat")

	result, err := app.Search(t.Context(), sdd.RequestIdentity{Subject: "reader"}, "base", sdd.SearchRequest{SyncMode: sdd.SearchSyncNone, Phrase: credential})

	if err != nil {
		t.Fatal(err)
	}
	if embeddings.queryEmbeds != 0 {
		t.Error("the phrase must not be embedded")
	}
	if !strings.Contains(result.Notice, "searched for it literally") || !strings.Contains(result.Results, "searched for it literally") {
		t.Errorf("the response should say the phrase was searched literally: %+v", result)
	}
	if strings.Contains(result.Notice, credential) {
		t.Error("the notice must never repeat the credential")
	}
}

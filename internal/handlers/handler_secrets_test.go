package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/handlers"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/test"
)

func fixtureCredential(t *testing.T, rule string) string {
	t.Helper()
	content, err := fs.ReadFile(test.SecretFixtures(), "positive/"+rule+".txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(content))
}

func graphFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// A credential is refused before pre-flight or summary send the entry to an
// LLM provider, and nothing of it lands on disk, not even the stdin retry copy.
func TestNewEntryRefusesACredentialBeforeAnyModelCall(t *testing.T) {
	credential := fixtureCredential(t, "github-pat")
	cases := []struct {
		name  string
		edit  func(cmd *command.NewEntryCmd, dir string)
		where string
	}{
		{name: "body", edit: func(cmd *command.NewEntryCmd, _ string) { cmd.Description = "Setup notes:\n" + credential }, where: "the body"},
		{name: "summary", edit: func(cmd *command.NewEntryCmd, _ string) { cmd.Summary = credential }, where: "the summary"},
		{name: "title", edit: func(cmd *command.NewEntryCmd, _ string) { cmd.Title = credential }, where: "the title"},
		{name: "attachment file", edit: func(cmd *command.NewEntryCmd, dir string) {
			source := filepath.Join(dir, "notes.txt")
			err := os.WriteFile(source, []byte(credential+"\n"), 0o644)
			if err != nil {
				panic(err)
			}
			cmd.Attachments = []command.Attachment{{Source: source, Target: "notes.txt"}}
		}, where: "the attachment notes.txt"},
		{name: "stdin attachment on a dry run", edit: func(cmd *command.NewEntryCmd, _ string) {
			cmd.DryRun = true
			cmd.Attachments = []command.Attachment{{Source: "-", Target: "log.txt", Data: []byte(credential + "\n")}}
		}, where: "the attachment log.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			graphDir := t.TempDir()
			sddDir := filepath.Join(t.TempDir(), ".sdd")
			sources := t.TempDir()
			reader := &fakeReader{preflightResult: &query.PreflightResult{}}
			committer := &recordingCommitter{}
			h := handlers.New(handlers.Options{
				GraphDir: graphDir, SDDDir: sddDir, Reader: reader, Committer: committer,
				LLMRunner: &trippingRunner{t: t}, Stderr: &bytes.Buffer{},
			})
			cmd := &command.NewEntryCmd{
				Type: model.TypeSignal, Layer: model.LayerTactical,
				Description: "A gap worth recording.", Summary: "A gap.", Title: "Gap",
			}
			tc.edit(cmd, sources)

			err := h.NewEntry(context.Background(), cmd)

			if !errors.Is(err, secrets.ErrCredential) {
				t.Fatalf("the capture should be refused as holding a credential: %v", err)
			}
			if !strings.Contains(err.Error(), tc.where) {
				t.Errorf("the refusal should name where the credential sits: %v", err)
			}
			if strings.Contains(err.Error(), credential) {
				t.Error("the refusal must never repeat the credential")
			}
			if reader.preflightCalls != 0 {
				t.Error("pre-flight should not run on a refused capture")
			}
			if files := graphFiles(t, graphDir); len(files) != 0 {
				t.Errorf("a refused capture should write nothing, wrote %v", files)
			}
			if _, statErr := os.Stat(sddDir); !os.IsNotExist(statErr) {
				t.Error("a refused capture should keep no stdin retry copy")
			}
			if committer.calls != 0 {
				t.Error("a refused capture should commit nothing")
			}
		})
	}
}

func TestNewEntryRefusesAGeneratedSummaryHoldingACredential(t *testing.T) {
	graphDir := t.TempDir()
	runner := &stubRunner{text: fixtureCredential(t, "gitlab-pat")}
	committer := &recordingCommitter{}
	h := handlers.New(handlers.Options{
		GraphDir: graphDir, SDDDir: filepath.Join(t.TempDir(), ".sdd"), Reader: &fakeReader{}, Committer: committer,
		LLMRunner: runner, Stderr: &bytes.Buffer{},
	})

	err := h.NewEntry(context.Background(), &command.NewEntryCmd{
		Type: model.TypeSignal, Layer: model.LayerTactical, Description: "A gap worth recording.", SkipPreflight: true,
	})

	if !errors.Is(err, secrets.ErrCredential) || !strings.Contains(err.Error(), "the summary") {
		t.Fatalf("a generated summary holding a credential should refuse the write: %v", err)
	}
	if files := graphFiles(t, graphDir); len(files) != 0 {
		t.Errorf("a refused capture should write nothing, wrote %v", files)
	}
}

func TestSummarizeRefusesACredential(t *testing.T) {
	credential := fixtureCredential(t, "slack-bot-token")
	cases := []struct {
		name   string
		cmd    func() *command.SummarizeCmd
		runner *stubRunner
		where  string
	}{
		{name: "explicit text", cmd: func() *command.SummarizeCmd {
			return &command.SummarizeCmd{EntryIDs: []string{summarizedEntry}, ExplicitText: &credential}
		}, where: "the summary"},
		{name: "explicit title", cmd: func() *command.SummarizeCmd {
			return &command.SummarizeCmd{EntryIDs: []string{summarizedEntry}, ExplicitTitle: &credential}
		}, where: "the title"},
		{name: "generated summary", cmd: func() *command.SummarizeCmd {
			return &command.SummarizeCmd{EntryIDs: []string{summarizedEntry}}
		}, runner: &stubRunner{text: credential}, where: "the summary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			graphDir := t.TempDir()
			writeSummarizePair(t, graphDir)
			before := readEntryFile(t, graphDir, summarizedEntry)
			options := handlers.Options{GraphDir: graphDir, Reader: &graphReader{}, Committer: &recordingCommitter{}}
			if tc.runner != nil {
				options.LLMRunner = tc.runner
			}
			h := handlers.New(options)

			err := h.Summarize(context.Background(), tc.cmd())

			if !errors.Is(err, secrets.ErrCredential) || !strings.Contains(err.Error(), tc.where) {
				t.Fatalf("the summary write should be refused naming the part: %v", err)
			}
			if readEntryFile(t, graphDir, summarizedEntry) != before {
				t.Error("a refused summary should leave the entry untouched")
			}
		})
	}
}

func TestStartWIPRefusesACredentialInTheDescription(t *testing.T) {
	graphDir := t.TempDir()
	writeSummarizePair(t, graphDir)
	committer := &recordingCommitter{}
	h := handlers.New(handlers.Options{GraphDir: graphDir, Reader: &graphReader{}, Committer: committer})

	err := h.StartWIP(context.Background(), &command.StartWIPCmd{
		EntryID: summarizedEntry, Participant: "Ada", Description: "Rotate " + fixtureCredential(t, "stripe-access-token"),
	})

	if !errors.Is(err, secrets.ErrCredential) || !strings.Contains(err.Error(), "the WIP description") {
		t.Fatalf("the marker should be refused naming its description: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(graphDir, "wip")); !os.IsNotExist(statErr) {
		t.Error("a refused marker should write nothing")
	}
	if committer.calls != 0 {
		t.Error("a refused marker should commit nothing")
	}
}

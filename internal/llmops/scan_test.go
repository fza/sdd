package llmops_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/pkg/llm"
	"github.com/networkteam/sdd/pkg/llm/relevance"
)

// The scan reads the written entry and the hidden entries in one call, each
// a label and its line with entry IDs stripped, and returns the IDs it
// flags in list order.
func TestScanJudge_FlagsDuplicatesAndContradictionsInOneCall(t *testing.T) {
	var got llm.Request
	runner := llm.RunnerFunc(func(_ context.Context, request llm.Request) (llm.Result, error) {
		got = request
		return llm.Result{Text: `{"flagged": ["e3", "e1"]}`}, nil
	})
	scan, err := llmops.NewScanJudge(runner)
	if err != nil {
		t.Fatal(err)
	}
	flagged, err := scan.Scan(t.Context(), "The written entry refines 20260101-000000-d-cpt-abc.", []relevance.Candidate{
		{ID: "a", Line: "[decision/directive] First: says the same."},
		{ID: "b", Line: "[signal/gap] Second: unrelated."},
		{ID: "c", Text: "[decision/plan] Third: rules it out, see 20260102-000000-s-tac-xyz."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(flagged, []string{"c", "a"}) {
		t.Fatalf("flagged %v: the scan should map each flagged label to its entry", flagged)
	}
	if got.Purpose != llm.PurposeScan || !strings.Contains(got.SystemPrompt, "duplicates the new entry") {
		t.Fatalf("request %+v should carry the scan purpose and prompt", got)
	}
	for _, want := range []string{"The written entry refines", "e1 [decision/directive] First: says the same.", "e3 [decision/plan] Third: rules it out, see ."} {
		if !strings.Contains(got.UserPrompt, want) {
			t.Errorf("user prompt lacks %q:\n%s", want, got.UserPrompt)
		}
	}
	if strings.Contains(got.UserPrompt, "20260101-000000-d-cpt-abc") || strings.Contains(got.UserPrompt, "20260102-000000-s-tac-xyz") {
		t.Fatalf("entry IDs should be stripped:\n%s", got.UserPrompt)
	}
	var schema struct {
		Properties struct {
			Flagged struct {
				Items struct {
					Enum []string `json:"enum"`
				} `json:"items"`
			} `json:"flagged"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(got.ResponseSchema, &schema); err != nil || !slices.Equal(schema.Properties.Flagged.Items.Enum, []string{"e1", "e2", "e3"}) {
		t.Fatalf("schema %s should hold the answer to the list's labels", got.ResponseSchema)
	}
}

func TestScanJudge_RefusesALabelOutsideTheList(t *testing.T) {
	runner := llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) {
		return llm.Result{Text: `{"flagged": ["e9"]}`}, nil
	})
	scan, err := llmops.NewScanJudge(runner)
	if err != nil {
		t.Fatal(err)
	}
	_, err = scan.Scan(t.Context(), "written", []relevance.Candidate{{ID: "a", Line: "a"}})
	var malformed *llm.MalformedAnswerError
	if !errors.As(err, &malformed) {
		t.Fatalf("err %v: a label outside the list should be a malformed answer", err)
	}
}

func TestScanJudge_ScansNothingWithoutHiddenEntries(t *testing.T) {
	runner := llm.RunnerFunc(func(context.Context, llm.Request) (llm.Result, error) {
		t.Fatal("no call should be sent")
		return llm.Result{}, nil
	})
	scan, err := llmops.NewScanJudge(runner)
	if err != nil {
		t.Fatal(err)
	}
	if flagged, err := scan.Scan(t.Context(), "written", nil); err != nil || flagged != nil {
		t.Fatalf("flagged %v, %v", flagged, err)
	}
}

func TestChatRevision_NamesThePrompt(t *testing.T) {
	revision := llmops.ChatRevision()
	if len(revision) != 8 || revision != llmops.ChatRevision() {
		t.Fatalf("revision %q: the prompt should hash to one stable revision", revision)
	}
}

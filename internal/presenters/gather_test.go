package presenters_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
)

// Only an entry served in full owes a reference or a verdict, so the heading
// over the candidate lines asks for no verdict per line.
func TestRenderGather_CandidateLinesOweNoVerdict(t *testing.T) {
	candidate := entry("20260101-100000-s-tac-aaa")
	result := &query.GatherResult{
		Graph:   model.NewGraph([]*model.Entry{candidate}),
		ToJudge: []model.GatherCandidate{{Entry: candidate, FoundBy: []string{"semantic"}}},
	}
	var out bytes.Buffer
	err := presenters.RenderGather(&out, result)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "# candidates (1)\n\nA line owes no verdict.") {
		t.Errorf("the candidate lines should open under a heading that owes no verdict:\n%s", text)
	}
	if strings.Contains(text, "to judge") {
		t.Errorf("the serve should not ask to judge each line:\n%s", text)
	}
}

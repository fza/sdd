package presenters_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
)

// Only a line marked likely bearing owes a verdict or a reference
// (20261121-223938-d-tac-rqi), so a line in doubt comes under a heading that
// asks for none.
func TestRenderGather_LinesInDoubtOweNoVerdict(t *testing.T) {
	marked, doubt := entry("20260101-100000-s-tac-aaa"), entry("20260101-110000-s-tac-bbb")
	result := &query.GatherResult{
		Graph: model.NewGraph([]*model.Entry{marked, doubt}),
		Lines: model.Lines{
			Bearing: []model.Line{{Entry: marked, Marked: true}},
			Doubt:   []model.Line{{Entry: doubt}},
		},
	}
	var out bytes.Buffer
	err := presenters.RenderGather(&out, result)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "## likely bearing (1) — each owes a verdict or a reference\n") {
		t.Errorf("the marked lines should open under a heading that names what they owe:\n%s", text)
	}
	_, inDoubt, ok := strings.Cut(text, "## in doubt (1)\n")
	if !ok || strings.Contains(inDoubt, "owe") {
		t.Errorf("the lines in doubt should come under a heading that asks for no verdict:\n%s", text)
	}
	if strings.Contains(text, "to judge") {
		t.Errorf("the serve should not ask to judge each line:\n%s", text)
	}
}

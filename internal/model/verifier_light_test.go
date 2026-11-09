package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func rankBy(word string) func(string) float64 {
	return func(p string) float64 {
		if strings.Contains(p, word) {
			return 1
		}
		return 0
	}
}

func TestLightBudgetChars(t *testing.T) {
	if got := model.LightBudgetChars(100000, 4000); got != 48000*3 {
		t.Fatalf("half the window left after the reserve, at 3 characters a token, got %d", got)
	}
	if got := model.LightBudgetChars(4000, 4000); got != 0 {
		t.Fatalf("a reserve filling the window leaves no room, got %d", got)
	}
}

func TestExcerptParagraphs_KeepsOrderHeadingsAndMarksGaps(t *testing.T) {
	body := strings.Join([]string{
		"First paragraph about nothing much at all.",
		"## Section",
		"Second paragraph names the target term.",
		"Third paragraph is filler text here.",
		"Fourth paragraph also names the target term.",
		"Fifth paragraph is more filler text.",
	}, "\n\n")
	want := strings.Join([]string{
		model.ExcerptGap(1),
		"## Section",
		"Second paragraph names the target term.",
		model.ExcerptGap(1),
		"Fourth paragraph also names the target term.",
		model.ExcerptGap(1),
	}, "\n\n")
	excerpt, ok := model.ExcerptParagraphs(body, len(want), rankBy("target"))
	if !ok {
		t.Fatal("two paragraphs fit the room")
	}
	if excerpt != want {
		t.Fatalf("the excerpt should keep the ranked paragraphs in document order with their heading and a marker per gap:\n%s", excerpt)
	}
}

func TestExcerptParagraphs_KeepsCodeBlocksAndListItemsWhole(t *testing.T) {
	body := "Intro text.\n\n```go\nfunc a() {\n\n\treturn\n}\n```\n\n- one item\n- two item named target\n\nTail."
	excerpt, ok := model.ExcerptParagraphs(body, 90, rankBy("target"))
	if !ok {
		t.Fatal("one list item fits")
	}
	if !strings.Contains(excerpt, "- two item named target") || strings.Contains(excerpt, "- one item") {
		t.Fatalf("a list item should be its own block:\n%s", excerpt)
	}
	excerpt, _ = model.ExcerptParagraphs(body, 200, rankBy("func"))
	if !strings.Contains(excerpt, "```go\nfunc a() {\n\n\treturn\n}\n```") {
		t.Fatalf("a fenced code block should stay whole, blank lines included:\n%s", excerpt)
	}
	fenced := "Intro.\n\n```sh\necho a\n\n# note\necho b\n```\n\nTail names target."
	excerpt, _ = model.ExcerptParagraphs(fenced, 52, rankBy("target"))
	if !strings.Contains(excerpt, "Tail names target.") || strings.Contains(excerpt, "# note") {
		t.Fatalf("a comment line inside a fence is no section heading:\n%s", excerpt)
	}
}

func TestExcerptParagraphs_CutsAnOversizedParagraphAtSentenceEnds(t *testing.T) {
	body := "Alpha sentence one is here. Beta sentence names target. Gamma sentence three is here. Delta sentence four is here too."
	excerpt, ok := model.ExcerptParagraphs(body, 100, rankBy("target"))
	if !ok {
		t.Fatal("one sentence fits")
	}
	if !strings.Contains(excerpt, "Beta sentence names target.") || strings.Contains(excerpt, "Gamma") || !strings.Contains(excerpt, "left out") {
		t.Fatalf("a paragraph larger than the room should be cut at sentence ends, the ranked sentence kept:\n%s", excerpt)
	}
	_, ok = model.ExcerptParagraphs("One long sentence without any end that cannot fit", 10, rankBy("x"))
	if ok {
		t.Fatal("nothing fitting the room should be reported")
	}
}

func lightItem(id, text string, related []string, topics ...string) model.LightItem {
	return model.LightItem{ID: id, Header: "ID: " + id, Text: text, Related: related, Topics: topics}
}

func callIDs(calls []model.LightCall) [][]string {
	var out [][]string
	for _, call := range calls {
		var ids []string
		for _, part := range call.Parts {
			ids = append(ids, part.ID)
		}
		out = append(out, ids)
	}
	return out
}

func TestPlanLightCalls_OneCallWhenItFits(t *testing.T) {
	calls, err := model.PlanLightCalls(10000, []model.LightItem{lightItem("a", "text", nil), lightItem("b", "text", nil)}, rankBy("x"))
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || len(calls[0].Parts) != 2 {
		t.Fatalf("a reading that fits goes in one call, got %v", callIDs(calls))
	}
}

func TestPlanLightCalls_GroupsRelatedEntries(t *testing.T) {
	text := strings.Repeat("x", 40)
	items := []model.LightItem{
		lightItem("a", text, nil, "topic/one"),
		lightItem("b", text, nil, "topic/two"),
		lightItem("c", text, []string{"a"}),
		lightItem("d", text, nil, "topic/two"),
	}
	calls, err := model.PlanLightCalls(160, items, rankBy("x"))
	if err != nil {
		t.Fatal(err)
	}
	got := callIDs(calls)
	if len(got) != 2 {
		t.Fatalf("four entries in two related pairs need two calls, got %v", got)
	}
	together := func(x, y string) bool {
		for _, ids := range got {
			if strings.Contains(strings.Join(ids, ","), x) && strings.Contains(strings.Join(ids, ","), y) {
				return true
			}
		}
		return false
	}
	if !together("a", "c") || !together("b", "d") {
		t.Fatalf("entries linked by a ref or a shared topic should share a call, got %v", got)
	}
}

func TestPlanLightCalls_ExcerptsAnOversizedEntryKeepingItsHeader(t *testing.T) {
	body := "Filler paragraph one with quite a lot of extra words in it.\n\nTarget paragraph two.\n\nFiller paragraph three with quite a lot of extra words too."
	calls, err := model.PlanLightCalls(120, []model.LightItem{lightItem("big", body, nil)}, rankBy("Target"))
	if err != nil {
		t.Fatal(err)
	}
	part := calls[0].Parts[0]
	if !part.Excerpted || part.Header != "ID: big" || !strings.Contains(part.Text, "Target paragraph two.") || !strings.Contains(part.Text, model.ExcerptGap(1)) {
		t.Fatalf("an oversized entry should travel as a marked excerpt beside its whole header: %+v", part)
	}
	_, err = model.PlanLightCalls(5, []model.LightItem{lightItem("big", body, nil)}, rankBy("Target"))
	if err == nil || !strings.Contains(err.Error(), "big") {
		t.Fatalf("a header larger than a call should be refused naming the entry, got %v", err)
	}
}

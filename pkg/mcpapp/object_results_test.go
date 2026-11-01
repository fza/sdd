package mcpapp_test

import (
	"strings"
	"testing"
)

// Under ServeInFull results carry objects instead of text
// (20261101-165616-d-tac-qtk); without it they stay text.
func TestObjectResultsReplaceTextOnlyWhenServedInFull(t *testing.T) {
	for _, inFull := range []bool{false, true} {
		name := "text"
		if inFull {
			name = "objects"
		}
		t.Run(name, func(t *testing.T) {
			env := newTestServer(t, nil, "", "", func(o *testServerOptions) { o.MCP.ServeInFull = inFull })
			cs := connect(t, env.srv)

			var opened map[string]any
			call(t, cs, "start_session", map[string]any{}, &opened)
			session, _ := opened["session"].(string)
			var shown, found, viewed, started, resumed map[string]any
			call(t, cs, "show", map[string]any{"session": session, "ids": []string{fixtureGapID}}, &shown)
			call(t, cs, "search", map[string]any{"session": session, "terms": []string{"oscillation"}}, &found)
			call(t, cs, "view", map[string]any{"session": session, "layout": "kind(gap):as-list"}, &viewed)
			call(t, cs, "start_procedure", map[string]any{"session": session, "canonical": "capture"}, &started)
			call(t, cs, "resume_session", map[string]any{"session": session}, &resumed)

			checks := []struct {
				result      map[string]any
				text, field string
			}{
				{opened, "instructions", "lanes"},
				{opened, "framing", "framing_blocks"},
				{shown, "entries", "items"},
				{found, "results", "hits"},
				{viewed, "sections", "view_sections"},
				{started, "instructions", "lanes"},
				{resumed, "instructions", "lanes"},
				{resumed, "framing", "framing_blocks"},
			}
			for _, check := range checks {
				_, hasObjects := check.result[check.field]
				text, _ := check.result[check.text].(string)
				if hasObjects != inFull {
					t.Errorf("%s should be present exactly when served in full", check.field)
				}
				if inFull && text != "" {
					t.Errorf("%s should be empty when %s carries the objects", check.text, check.field)
				}
				if !inFull && text == "" {
					t.Errorf("%s should carry the text when not served in full", check.text)
				}
			}
			if !inFull {
				return
			}

			items, _ := shown["items"].([]any)
			if len(items) != 1 {
				t.Fatalf("show should carry one item per shown entry, got %v", shown["items"])
			}
			item, _ := items[0].(map[string]any)
			if item["id"] != fixtureGapID || item["kind"] != "gap" || item["status"] != "open" {
				t.Errorf("the item should name the entry, its kind and status, got %v", item)
			}
			if body, _ := item["body"].(string); !strings.Contains(body, "contradictory findings across runs on identical input") {
				t.Errorf("the item should carry the full body, got %q", body)
			}
			hits, _ := found["hits"].([]any)
			if len(hits) == 0 {
				t.Fatal("search should carry the match as a hit")
			}
			if hit, _ := hits[0].(map[string]any); hit["id"] != fixtureGapID || hit["summary"] == "" {
				t.Errorf("a hit should name the entry and its summary, got %v", hit)
			}
			sections, _ := viewed["view_sections"].([]any)
			if len(sections) != 1 {
				t.Fatalf("view should carry one object per layout section, got %v", viewed["view_sections"])
			}
			section, _ := sections[0].(map[string]any)
			list, _ := section["list"].(map[string]any)
			listed, _ := list["items"].([]any)
			if section["render"] != "as-list" || len(listed) == 0 {
				t.Fatalf("the section should name its render and carry its entries as a list, got %v", section)
			}
			listItem, _ := listed[0].(map[string]any)
			if entry, _ := listItem["entry"].(map[string]any); entry["id"] != fixtureGapID {
				t.Errorf("a list item should name its entry, got %v", listItem)
			}
			requireNamedBlocks(t, opened["framing_blocks"], "info")
			requireNamedBlocks(t, started["lanes"], "assemble", "diagnostics")
		})
	}
}

func requireNamedBlocks(t *testing.T, field any, names ...string) {
	t.Helper()
	blocks, _ := field.([]any)
	if len(blocks) == 0 {
		t.Fatalf("expected named blocks, got %v", field)
	}
	seen := map[string]bool{}
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		name, _ := block["name"].(string)
		text, _ := block["text"].(string)
		if name == "" || text == "" {
			t.Errorf("every block should carry a name and its text, got %v", block)
		}
		seen[name] = true
	}
	for _, name := range names {
		if !seen[name] {
			t.Errorf("the blocks should include %q, got %v", name, field)
		}
	}
}

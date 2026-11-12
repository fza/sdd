package mcpapp

import (
	"encoding/json"
	"fmt"

	"github.com/networkteam/sdd/internal/model"
)

// The result bound (20261112-075604-d-tac-t4r, model.MaxResultChars) leaves
// room past what a result's parts may take: textBudget for the one large text
// field of a read, serveBudget for a serve before its notices join it.
const (
	textBudget  = model.MaxResultChars - 3000
	serveBudget = model.MaxResultChars - 2000
)

// resultChars is what v costs as a tool result: the text content carrying its
// JSON, which Claude Code counts against the bound.
func resultChars(v any) (int, error) {
	structured, err := json.Marshal(v)
	if err != nil {
		return 0, fmt.Errorf("measuring a result: %w", err)
	}
	return len(structured), nil
}

// pagedText bounds text, the large field of a tool's result, to textBudget:
// text that fits serves whole; else its pages are held for the session and
// the first serves, ending in the call that serves the next.
func pagedText(ss *shellSession, tool, text string) string {
	if model.ServedChars(text) <= textBudget {
		delete(ss.pages, tool)
		return text
	}
	if ss.pages == nil {
		ss.pages = map[string][]string{}
	}
	ss.pages[tool] = model.SplitServed(text, textBudget-500)
	return pageText(ss, tool, 1)
}

// heldPage serves a later page of tool's last paged result.
func heldPage(ss *shellSession, tool string, page int) (string, error) {
	if page > len(ss.pages[tool]) {
		return "", toolError("no page %d of a %s result is held: call %s again without page", page, tool, tool)
	}
	return pageText(ss, tool, page), nil
}

func pageText(ss *shellSession, tool string, page int) string {
	pages := ss.pages[tool]
	if page == len(pages) {
		return fmt.Sprintf("%s\n\n(page %d of %d of this %s result, its last)", pages[page-1], page, len(pages), tool)
	}
	return fmt.Sprintf("%s\n\n(page %d of %d of this %s result — call %s again with the same arguments and page %d for the next; it serves the held result without running the read again)", pages[page-1], page, len(pages), tool, tool, page+1)
}

// pagedItems splits a result's list into pages that each cost at most
// textBudget, keeping every item whole; an empty list is one empty page.
func pagedItems[T any](items []T) ([][]T, error) {
	pages := [][]T{nil}
	spent := 0
	for _, item := range items {
		cost, err := resultChars(item)
		if err != nil {
			return nil, err
		}
		if spent > 0 && spent+cost > textBudget {
			pages, spent = append(pages, nil), 0
		}
		pages[len(pages)-1] = append(pages[len(pages)-1], item)
		spent += cost
	}
	return pages, nil
}

// boundedPage ends an attachment page whose content would cost more than
// textBudget at its last line within it, next_offset continuing there.
func boundedPage(page ReadAttachmentResult) ReadAttachmentResult {
	if model.ServedChars(page.Content) <= textBudget {
		return page
	}
	page.Content = model.SplitServed(page.Content, textBudget)[0]
	page.NextOffset, page.More = page.Offset+int64(len(page.Content)), true
	return page
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

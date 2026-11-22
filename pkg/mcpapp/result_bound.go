package mcpapp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/model"
)

// The result bound (model.MaxResultChars) leaves room past what a result's
// parts may take: textBudget for the one large text field of a read,
// serveBudget (model.MaxServeChars) for a serve.
const (
	textBudget  = model.MaxResultChars - 3000
	serveBudget = model.MaxServeChars
)

// resultChars is what v costs as a tool result: the text content carrying its
// JSON, which the bound counts.
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
	page := pageText(ss, tool, 1)
	if ss.repeatsPaged {
		page = fmt.Sprintf("(page 1 again: call %s with the same arguments and page 2 for the rest)\n\n%s", tool, page)
	}
	ss.pagedCall = ss.call
	return page
}

// callKey is what a repeat of req shares with it: the tool and its arguments
// but page, the answer and reread, which change between repeats.
func callKey(req *mcp.CallToolRequest) string {
	if req == nil || req.Params == nil {
		return ""
	}
	var args map[string]any
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return ""
	}
	for _, field := range []string{"page", "answer", "reread"} {
		delete(args, field)
	}
	key, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return req.Params.Name + string(key)
}

// pagedRead is pagedText for a read that logs the entries it serves: a paged
// result logs its bodies as read in full only once every page has served,
// so refsInspected never passes on a body the agent did not get.
func (s *toolSet) pagedRead(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, tool, text string, full, summary []string) (string, error) {
	text = pagedText(ss, tool, text)
	delete(ss.pageReads, tool)
	if pages := len(ss.pages[tool]); pages > 1 && len(full) > 0 {
		unserved := map[int]bool{}
		for page := 2; page <= pages; page++ {
			unserved[page] = true
		}
		if ss.pageReads == nil {
			ss.pageReads = map[string]pageRead{}
		}
		ss.pageReads[tool] = pageRead{full: full, unserved: unserved}
		full = nil
	}
	return text, s.logRootRead(ctx, req, ss, tool, full, summary)
}

// heldPageRead is heldPage for a read pagedRead logged, logging its bodies
// once the last page not yet served serves.
func (s *toolSet) heldPageRead(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, tool string, page int) (string, error) {
	text, err := heldPage(ss, tool, page)
	if err != nil {
		return "", err
	}
	pending, ok := ss.pageReads[tool]
	if !ok {
		return text, nil
	}
	delete(pending.unserved, page)
	if len(pending.unserved) > 0 {
		return text, nil
	}
	delete(ss.pageReads, tool)
	return text, s.logRootRead(ctx, req, ss, tool, pending.full, nil)
}

// heldPage serves a later page of tool's last paged result.
func heldPage(ss *shellSession, tool string, page int) (string, error) {
	if page > len(ss.pages[tool]) {
		return "", toolError("no page %d of a %s result is held: call %s again without page", page, tool, tool)
	}
	ss.pagedCall = ss.call
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

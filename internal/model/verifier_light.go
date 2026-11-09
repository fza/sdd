package model

import (
	"fmt"
	"slices"
	"strings"
)

// Light verifier sizing (20261109-004729-d-cpt-xs5, V-D12, V-D13): a call
// fills half the window left after the answer reserve, and a token counts as
// 3 characters.
const (
	LightCharsPerToken = 3
	LightFillRatio     = 0.5
	// LightUncappedOutput is the answer reserve when llm.max_tokens sends no
	// cap and the provider reports no output limit, and the cap on a
	// reported one.
	LightUncappedOutput = 32000
)

// LightBudgetChars is the characters one light call may hold for a window
// of windowTokens and an answer reserve of outputTokens.
func LightBudgetChars(windowTokens, outputTokens int) int {
	free := windowTokens - outputTokens
	if free <= 0 {
		return 0
	}
	return int(float64(free)*LightFillRatio) * LightCharsPerToken
}

// LightRole is an entry's place in the grounding reading a light call
// judges the draft against.
type LightRole string

const (
	LightRoleFull     LightRole = "read in full"
	LightRoleBrief    LightRole = "brief"
	LightRoleSetAside LightRole = "set aside"
)

// LightItem is one reading entry as a light call carries it: a header never
// cut, and a text (the body, or the brief) cut into excerpts only when the
// item alone exceeds a call.
type LightItem struct {
	ID     string
	Header string
	Text   string
	// Related are the IDs this entry refs; entries connected through refs,
	// else sharing a topic, go into the same call.
	Related []string
	Topics  []string
}

// LightPart is one item placed in a call.
type LightPart struct {
	ID        string
	Header    string
	Text      string
	Excerpted bool
}

// Size is the characters the part adds to a call.
func (p LightPart) Size() int {
	return len(p.Header) + len(p.Text) + lightPartSeparator
}

const lightPartSeparator = len("\n\n---\n\n") + 1

// LightCall is the reading entries one light call carries.
type LightCall struct {
	Parts []LightPart
}

// PlanLightCalls packs the reading into the fewest calls of room characters
// each (V-D11): entries related through refs, else a shared topic, are
// grouped and whole groups placed together where they fit; an item larger
// than a call alone is cut into excerpts ranked by rank. Every item's header
// stays whole, so a header larger than a call refuses.
func PlanLightCalls(room int, items []LightItem, rank func(paragraph string) float64) ([]LightCall, error) {
	parts := make([]LightPart, len(items))
	for i, item := range items {
		part := LightPart{ID: item.ID, Header: item.Header, Text: item.Text}
		if part.Size() > room {
			textRoom := room - len(item.Header) - lightPartSeparator
			excerpt, ok := ExcerptParagraphs(item.Text, textRoom, rank)
			if !ok {
				return nil, fmt.Errorf("entry %s does not fit one call even as one paragraph beside its header", item.ID)
			}
			part.Text, part.Excerpted = excerpt, true
		}
		parts[i] = part
	}
	var calls []LightCall
	var sizes []int
	place := func(group []int) {
		total := 0
		for _, i := range group {
			total += parts[i].Size()
		}
		if total <= room {
			for c := range calls {
				if sizes[c]+total <= room {
					for _, i := range group {
						calls[c].Parts = append(calls[c].Parts, parts[i])
					}
					sizes[c] += total
					return
				}
			}
			call := LightCall{}
			for _, i := range group {
				call.Parts = append(call.Parts, parts[i])
			}
			calls, sizes = append(calls, call), append(sizes, total)
			return
		}
		current := -1
		for _, i := range group {
			if current < 0 || sizes[current]+parts[i].Size() > room {
				calls, sizes = append(calls, LightCall{}), append(sizes, 0)
				current = len(calls) - 1
			}
			calls[current].Parts = append(calls[current].Parts, parts[i])
			sizes[current] += parts[i].Size()
		}
	}
	groups := lightGroups(items)
	slices.SortStableFunc(groups, func(a, b []int) int {
		return groupSize(parts, b) - groupSize(parts, a)
	})
	for _, group := range groups {
		place(group)
	}
	return calls, nil
}

func groupSize(parts []LightPart, group []int) int {
	total := 0
	for _, i := range group {
		total += parts[i].Size()
	}
	return total
}

// lightGroups unions items connected by a ref between them, else by a
// shared topic, keeping each group in item order.
func lightGroups(items []LightItem) [][]int {
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			if ra < rb {
				parent[rb] = ra
			} else {
				parent[ra] = rb
			}
		}
	}
	index := make(map[string]int, len(items))
	for i, item := range items {
		index[item.ID] = i
	}
	byTopic := map[string]int{}
	for i, item := range items {
		for _, id := range item.Related {
			if j, ok := index[id]; ok {
				union(i, j)
			}
		}
		for _, topic := range item.Topics {
			if j, ok := byTopic[topic]; ok {
				union(i, j)
			} else {
				byTopic[topic] = i
			}
		}
	}
	var order []int
	members := map[int][]int{}
	for i := range items {
		root := find(i)
		if _, ok := members[root]; !ok {
			order = append(order, root)
		}
		members[root] = append(members[root], i)
	}
	groups := make([][]int, 0, len(order))
	for _, root := range order {
		groups = append(groups, members[root])
	}
	return groups
}

// excerptBlock is one whole paragraph, code block or list item of a body,
// with the heading of the section it sits in.
type excerptBlock struct {
	text    string
	heading int
}

// ExcerptGap is the marker for blocks left out of an excerpt.
func ExcerptGap(n int) string {
	if n == 1 {
		return "[… 1 paragraph left out …]"
	}
	return fmt.Sprintf("[… %d paragraphs left out …]", n)
}

// ExcerptParagraphs cuts body to room characters as whole paragraphs, code
// blocks and list items (V-D13): the blocks rank scores highest are kept, in
// document order, with the heading of every section they draw from, and a
// marker names each gap at the start, between kept blocks and at the end. A
// block larger than room alone is cut at sentence ends. It reports false
// when not even one block fits.
func ExcerptParagraphs(body string, room int, rank func(paragraph string) float64) (string, bool) {
	blocks := splitExcerptBlocks(body, room)
	if len(blocks) == 0 {
		return "", room >= 0
	}
	type scored struct {
		index int
		score float64
	}
	var candidates []scored
	for i, block := range blocks {
		if block.heading == i {
			continue
		}
		candidates = append(candidates, scored{i, rank(block.text)})
	}
	slices.SortStableFunc(candidates, func(a, b scored) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		}
		return a.index - b.index
	})
	kept := map[int]bool{}
	for _, candidate := range candidates {
		trial := withBlock(kept, candidate.index, blocks)
		if len(renderExcerpt(blocks, trial)) <= room {
			kept = trial
		}
	}
	if !slices.ContainsFunc(candidates, func(c scored) bool { return kept[c.index] }) {
		return "", false
	}
	return renderExcerpt(blocks, kept), true
}

// withBlock returns kept with index and the heading of its section added.
func withBlock(kept map[int]bool, index int, blocks []excerptBlock) map[int]bool {
	trial := make(map[int]bool, len(kept)+2)
	for k := range kept {
		trial[k] = true
	}
	trial[index] = true
	if h := blocks[index].heading; h >= 0 {
		trial[h] = true
	}
	return trial
}

func renderExcerpt(blocks []excerptBlock, kept map[int]bool) string {
	var out []string
	gap := 0
	for i, block := range blocks {
		if !kept[i] {
			if block.heading != i {
				gap++
			}
			continue
		}
		if gap > 0 {
			out = append(out, ExcerptGap(gap))
			gap = 0
		}
		out = append(out, block.text)
	}
	if gap > 0 {
		out = append(out, ExcerptGap(gap))
	}
	return strings.Join(out, "\n\n")
}

// splitExcerptBlocks splits body at blank lines into whole blocks, keeping a
// fenced code block whole and each list item apart; a block longer than room
// is cut at sentence ends.
func splitExcerptBlocks(body string, room int) []excerptBlock {
	var raw []string
	var current []string
	inFence := false
	flush := func() {
		if len(current) > 0 {
			raw = append(raw, strings.Join(current, "\n"))
			current = nil
		}
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if !inFence {
				flush()
			}
			current = append(current, line)
			inFence = !inFence
			if !inFence {
				flush()
			}
			continue
		}
		if inFence {
			current = append(current, line)
			continue
		}
		switch {
		case trimmed == "":
			flush()
		case isListItem(trimmed) || strings.HasPrefix(trimmed, "#"):
			flush()
			current = append(current, line)
			if strings.HasPrefix(trimmed, "#") {
				flush()
			}
		default:
			current = append(current, line)
		}
	}
	flush()
	var blocks []excerptBlock
	heading := -1
	for _, text := range raw {
		if strings.HasPrefix(strings.TrimSpace(text), "#") {
			heading = len(blocks)
			blocks = append(blocks, excerptBlock{text: text, heading: heading})
			continue
		}
		pieces := []string{text}
		if len(text) > room && !strings.HasPrefix(strings.TrimSpace(text), "```") {
			// Each sentence is its own block, so the ranked ones fit beside the gap markers.
			pieces = splitSentences(strings.Join(strings.Fields(text), " "))
		}
		for _, piece := range pieces {
			blocks = append(blocks, excerptBlock{text: piece, heading: heading})
		}
	}
	return blocks
}

func isListItem(line string) bool {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
		return true
	}
	digits := 0
	for digits < len(line) && line[digits] >= '0' && line[digits] <= '9' {
		digits++
	}
	return digits > 0 && digits < len(line) && (line[digits] == '.' || line[digits] == ')')
}

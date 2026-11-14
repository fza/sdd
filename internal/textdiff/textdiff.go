// Package textdiff renders a content-anchored, unified-style line diff
// without line numbers: hunks are anchored by their context lines alone, so
// the output stays meaningful for a reader who holds the text but not its
// numbering (20260826-120330-d-tac-8f8).
package textdiff

import (
	"slices"
	"strconv"
	"strings"
)

const contextLines = 2

// Unified returns the line diff from old to new: hunks headed by a bare
// "@@", context prefixed with a space, removals with "-", additions with
// "+". Empty when the texts are equal.
func Unified(old, new string) string {
	if old == new {
		return ""
	}
	a := strings.Split(old, "\n")
	b := strings.Split(new, "\n")
	ops := diffOps(a, b)

	var sb strings.Builder
	i := 0
	for i < len(ops) {
		// Skip runs of equal lines that are entirely outside any hunk.
		if ops[i].kind == opEqual {
			i++
			continue
		}
		// Start of a hunk: back up for leading context.
		start := i
		for back := 0; start > 0 && ops[start-1].kind == opEqual && back < contextLines; back++ {
			start--
		}
		// Extend to the hunk's end: include trailing context, merging change
		// runs separated by at most 2*contextLines equal lines.
		end := i
		for j := i; j < len(ops); {
			if ops[j].kind != opEqual {
				end = j + 1
				j++
				continue
			}
			run := 0
			for j+run < len(ops) && ops[j+run].kind == opEqual {
				run++
			}
			if j+run < len(ops) && run <= 2*contextLines {
				j += run
				continue
			}
			if trail := min(run, contextLines); j+trail <= len(ops) {
				end = j + trail
			}
			break
		}
		sb.WriteString("@@\n")
		for _, op := range ops[start:end] {
			switch op.kind {
			case opEqual:
				sb.WriteString(" " + op.line + "\n")
			case opDelete:
				sb.WriteString("-" + op.line + "\n")
			case opInsert:
				sb.WriteString("+" + op.line + "\n")
			}
		}
		i = end
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type op struct {
	kind opKind
	line string
}

// diffOps computes a line-level LCS edit script.
func diffOps(a, b []string) []op {
	// lcs[i][j] = length of the LCS of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	ops := make([]op, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{opEqual, a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{opDelete, a[i]})
			i++
		default:
			ops = append(ops, op{opInsert, b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, op{opDelete, a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, op{opInsert, b[j]})
	}
	return ops
}

// HeadTail returns text bounded to its first head and last tail lines, the
// elided middle marked with a line count. Text short enough returns whole.
func HeadTail(text string, head, tail int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= head+tail+1 {
		return text
	}
	elided := len(lines) - head - tail
	var parts []string
	parts = append(parts, lines[:head]...)
	parts = append(parts, "[… "+strconv.Itoa(elided)+" lines elided …]")
	parts = append(parts, lines[len(lines)-tail:]...)
	return strings.Join(parts, "\n")
}

// Span is the byte range [Start, End) of a text.
type Span struct{ Start, End int }

// ChangedSpans returns the spans of old that the change to new replaces, in
// order, each narrowed to the bytes that differ; a run of lines new inserts
// without replacing any is an empty span where it goes.
func ChangedSpans(old, new string) []Span {
	var spans []Span
	var removed, added []string
	offset, start := 0, 0
	flush := func() {
		if removed == nil && added == nil {
			return
		}
		before, after := strings.Join(removed, "\n"), strings.Join(added, "\n")
		if removed == nil {
			spans = append(spans, Span{Start: start, End: start})
		} else {
			prefix := commonPrefix(before, after)
			suffix := commonPrefix(reverse(before[prefix:]), reverse(after[prefix:]))
			spans = append(spans, Span{Start: start + prefix, End: start + len(before) - suffix})
		}
		removed, added = nil, nil
	}
	for _, o := range diffOps(strings.Split(old, "\n"), strings.Split(new, "\n")) {
		switch o.kind {
		case opEqual:
			flush()
			offset += len(o.line) + 1
		case opDelete:
			if removed == nil && added == nil {
				start = offset
			}
			removed = append(removed, o.line)
			offset += len(o.line) + 1
		case opInsert:
			if removed == nil && added == nil {
				start = offset
			}
			added = append(added, o.line)
		}
	}
	flush()
	return spans
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func reverse(s string) string {
	b := []byte(s)
	slices.Reverse(b)
	return string(b)
}

// ChangedPassages returns the runs of lines new holds that old does not, in
// order, each run joined by line breaks; blank lines alone are no passage.
func ChangedPassages(old, new string) []string {
	var passages []string
	var run []string
	flush := func() {
		if strings.TrimSpace(strings.Join(run, "")) != "" {
			passages = append(passages, strings.Join(run, "\n"))
		}
		run = nil
	}
	for _, o := range diffOps(strings.Split(old, "\n"), strings.Split(new, "\n")) {
		switch o.kind {
		case opInsert:
			run = append(run, o.line)
		case opEqual:
			flush()
		}
	}
	flush()
	return passages
}

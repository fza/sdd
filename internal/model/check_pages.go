package model

import "slices"

// LatePage is what the lines a check holds for its later pages become under
// the scores that arrived since (20261121-223938-d-tac-rqi).
type LatePage struct {
	// Bearing are held lines a late score places as likely bearing, marked,
	// by falling score.
	Bearing []Line
	// SetAside names held lines a late score sets aside.
	SetAside []string
	// Held are the lines still held, in their order, each with its late score.
	Held []Line
}

// ScoreHeld places the lines a check holds by the scores known now. Only a
// line without a score takes one. A late score at or above the relevant
// threshold, or without valid thresholds at or above FallbackMarkScore while
// the check marks fewer than FallbackMarks lines, makes its line likely
// bearing; one below the set-aside threshold sets it aside. bearing counts
// the lines the check marked so far.
func ScoreHeld(held []Line, scores map[string]uint8, thresholds RelevanceThresholds, bearing int) LatePage {
	var page LatePage
	for _, line := range held {
		score, late := scores[line.Entry.ID]
		if line.Score != nil || !late || line.Blind {
			page.Held = append(page.Held, line)
			continue
		}
		value := ScoreValue(score)
		line.Score = &value
		switch {
		case thresholds.Valid && value >= thresholds.Relevant, !thresholds.Valid && value >= FallbackMarkScore:
			line.Marked, line.Summary = true, true
			page.Bearing = append(page.Bearing, line)
		case thresholds.Valid && value < thresholds.SetAside:
			page.SetAside = append(page.SetAside, line.Entry.ID)
		default:
			page.Held = append(page.Held, line)
		}
	}
	slices.SortStableFunc(page.Bearing, byFallingScore)
	if !thresholds.Valid && bearing+len(page.Bearing) > FallbackMarks {
		keep := max(FallbackMarks-bearing, 0)
		var demoted []Line
		for _, line := range page.Bearing[keep:] {
			line.Marked = false
			demoted = append(demoted, line)
		}
		page.Bearing = page.Bearing[:keep]
		page.Held = append(demoted, page.Held...)
	}
	return page
}

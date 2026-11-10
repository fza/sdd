package llmops

import "strings"

// The quote contract (20261005-170145-d-tac-gch): a quote need not match its
// passage character for character, only locate it.

var quoteNormalizer = strings.NewReplacer(
	"„", "", "“", "", "”", "", "‚", "", "‘", "", "’", "", "«", "", "»", "",
	"‹", "", "›", "", `"`, "", "'", "", "`", "", "—", "-", "–", "-",
)

func normalizeQuoted(s string) string {
	return strings.Join(strings.Fields(quoteNormalizer.Replace(strings.ToLower(s))), " ")
}

// quoteParts splits a quote shortened with an ellipsis into the parts that
// must appear in order.
func quoteParts(quote string) []string {
	var parts []string
	for p := range strings.SplitSeq(strings.ReplaceAll(quote, "...", "…"), "…") {
		if n := normalizeQuoted(p); n != "" {
			parts = append(parts, n)
		}
	}
	return parts
}

// QuoteLocatesIn reports whether quote locates a passage of text that
// overlaps passage; an empty passage accepts any location.
func QuoteLocatesIn(text, quote, passage string) bool {
	nt := normalizeQuoted(text)
	from, to := 0, len(nt)
	if passage != "" {
		np := normalizeQuoted(passage)
		i := strings.Index(nt, np)
		if np == "" || i < 0 {
			return false
		}
		from, to = i, i+len(np)
	}
	parts := quoteParts(quote)
	if len(parts) == 0 {
		return false
	}
	for start := 0; ; {
		i := strings.Index(nt[start:], parts[0])
		if i < 0 {
			return false
		}
		first := start + i
		end := first + len(parts[0])
		for _, p := range parts[1:] {
			j := strings.Index(nt[end:], p)
			if j < 0 {
				return false
			}
			end += j + len(p)
		}
		if first < to && end > from {
			return true
		}
		start = first + 1
	}
}

// UnlocatedNote opens the observation of a pre-flight finding whose quote
// locates nothing pre-flight read: its severity is capped below blocking.
const UnlocatedNote = "Unverified, its quote locates no passage pre-flight read, so it does not block: "

// flagUnlocated keeps every finding and marks one whose quote locates no
// passage of material, capping it at medium: a model can invent a finding,
// and an invented one must never block a write.
func flagUnlocated(findings []Finding, material string) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if !QuoteLocatesIn(material, f.Quote, "") {
			if f.Severity == SeverityHigh {
				f.Severity = SeverityMedium
			}
			f.Observation = UnlocatedNote + f.Observation
		}
		out = append(out, f)
	}
	return out
}

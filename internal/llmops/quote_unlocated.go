package llmops

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

package presenters

import (
	"fmt"
	"io"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// RenderHiding writes the hiding rules of a status: each rule's step and
// the proof it rests on, what it hid and would have hidden, its misses by
// detector beside the scan's detection rate on planted misses, and the
// margins of references above the threshold's cut.
func RenderHiding(w io.Writer, report model.HidingReport) {
	const indent = "         "
	revision := report.Revision
	if revision == "" {
		revision = "none"
	}
	fmt.Fprintf(w, "hiding:  rules hide only on a replay proof for judge %s, prompt revision %s, on %s live entries\n", report.Judge, revision, thousands(report.GraphSize))
	detection := "no planted miss yet"
	if report.Planted > 0 {
		detection = fmt.Sprintf("the scan caught %.0f%% of %s planted", report.Detection*100, thousands(report.Planted))
	}
	for _, rule := range report.Rules {
		fmt.Fprintf(w, "%s%s: %s · %s\n", indent, rule.Rule, rule.State.Step, proofText(rule.State))
		fmt.Fprintf(w, "%s  hid %s in %s checks, would have hidden %s at full · misses: %s (%s)\n",
			indent, thousands(rule.Hid), thousands(rule.Checks), thousands(rule.Would), missesText(rule.Misses), detection)
	}
	margins := report.Margins
	if margins.Count == 0 {
		fmt.Fprintf(w, "%smargins: no reference scored against a cut yet\n", indent)
		return
	}
	fmt.Fprintf(w, "%smargins: of %s references, %s scored below the cut and %s within %.2f above it; a warning only, it moves no rule\n",
		indent, thousands(margins.Count), thousands(margins.Below), thousands(margins.Close), model.MarginWarn)
}

func proofText(state model.RuleState) string {
	proof := state.Proof
	var text string
	switch {
	case proof.Valid():
		text = fmt.Sprintf("proof: %s of %s needed entries hidden in %s replays, bound %.3f, graph of %s", thousands(proof.Hidden), thousands(proof.Needed), thousands(proof.Checks), proof.Bound, thousands(proof.GraphSize))
	case proof.Expired:
		text = "proof expired: the prompt changed or the graph grew by a quarter; it hides as its step allows until a fresh replay"
	default:
		text = "no proof yet"
	}
	if proof.Rule == model.RuleFarFloor && model.ProvenStep(proof) == model.StepNarrowed && !(proof.Valid() && proof.Bound >= model.ProofNarrowed) {
		text += "; careful form from the first check"
	}
	if state.Captures > 0 {
		text += fmt.Sprintf("; %d misses from %d captures since its latest replay", state.Misses, state.Captures)
	}
	return text
}

func missesText(misses map[model.MissDetector]int) string {
	var parts []string
	for _, detector := range []model.MissDetector{model.DetectRescue, model.DetectVerifier, model.DetectScan} {
		parts = append(parts, fmt.Sprintf("%d %s", misses[detector], detector))
	}
	return strings.Join(parts, ", ")
}

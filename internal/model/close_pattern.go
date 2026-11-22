package model

// StandardClose reports whether an entry of closerType and closerKind closing
// one of closedType and closedKind follows a standard close pattern:
//
//   - a decision closes a signal (a directive, plan or activity addressing it)
//   - a done signal closes a decision (completion record)
//   - a done signal closes a gap signal (short loop)
//   - a fact or insight signal closes a question signal (dissolution)
//   - a directive closes a fact or insight signal (retirement with rationale)
//   - a directive closes any decision (retirement without replacement)
//
// Any other pair is unusual: allowed where validateCloses allows it, but
// pre-flight's close-pattern rule asks whether the body states why. An empty
// kind counts as its type's default.
func StandardClose(closerType EntryType, closerKind Kind, closedType EntryType, closedKind Kind) bool {
	if closerKind == "" {
		closerKind = DefaultKindForType(closerType)
	}
	if closedKind == "" {
		closedKind = DefaultKindForType(closedType)
	}
	switch {
	case closerType == TypeDecision && closedType == TypeSignal:
		return true
	case closerType == TypeDecision && closerKind == KindDirective && closedType == TypeDecision:
		return true
	case closerType == TypeSignal && closerKind == KindDone:
		return closedType == TypeDecision || (closedType == TypeSignal && closedKind == KindGap)
	case closerType == TypeSignal && (closerKind == KindFact || closerKind == KindInsight):
		return closedType == TypeSignal && closedKind == KindQuestion
	default:
		return false
	}
}

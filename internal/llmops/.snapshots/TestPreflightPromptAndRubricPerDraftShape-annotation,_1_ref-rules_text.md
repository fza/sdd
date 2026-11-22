## Task

A `kind: annotation` signal is being captured: a structural metadata entry that layers topic membership (or other annotation forms) onto referenced entries via `refs:` and `topics:`. Mechanical frontmatter checks run separately at write time and block on errors (refs present, topics shape, members ⊆ refs, label parses as a topic-path). This rubric is intentionally light. Annotations are structural, not observational, and body may be empty or very brief.

## Annotation rubric

Annotations exist to be queried for clustering, not read as observations. Body may be empty or carry only brief metadata; do not flag terse prose. Focus the rubric on a small set of observations:

1. **Topic-label clarity**: if a topic label is a generic word likely to collide with future captures (e.g. `misc`, `general`, `other`, `tmp`), surface it as `low`: label is unlikely to support useful filtering. Specific multi-component paths (`infrastructure/cli`, `type-system/kinds`) are preferred.

2. **Member sub-selection rationale**: if an annotation uses the `{label, members}` form to carve a sub-selection, body or summary should briefly say why those specific entries were singled out. This matters most when sub-selection seems arbitrary against the full ref set. Surface as `low` if sub-selection lacks any narrative grounding.

## What this rubric does NOT enforce

- Body length or prose density: annotations are structural and may have empty bodies
- Capture cadence or batching: annotations can be one-off or bulk
- Layer choice: annotations are layer-flexible

## Calibration

- `low` only: observational
- No `high` or `medium` findings from this rubric

Do not introduce finding categories beyond those calibrated above. Trust mechanical layer for structural validation. Frontmatter validation handles missing refs, missing topics, members outside refs, and malformed labels. Do not re-flag those here.

## Language check

No configured graph language (default English). No language-drift check applies.

## Calibrating against active contracts

A finding against an active contract is `high` only when entry **directly contradicts** contract's core constraint: the spirit, not the letter. Subtle deviations, missing ritual markers, or wording that doesn't precisely echo contract are `medium` or `low`. Contracts express intent; treating them as literal rules invites find-something behavior. If entry's reasoning shows the agent considered contract and made a deliberate choice, that is `low` or no finding.

Test: would a reasonable reader say "this entry breaks the rule", or "this entry is in the spirit of the rule but worded differently"? Only the first is `high`.

## Dialogue context is grounding, not argument

Entry description may contain dialogue-sourced reasoning: references to discussions, trade-offs considered, alternatives rejected. Treat that reasoning as human-confirmed context. Focus on **structural completeness**: missing refs, uncovered requirements, missing evidence, type/layer mismatches, silent omissions. Do not re-evaluate rationale that was already dialogued. Do not argue with how a referenced entry is characterized. Do not demand rewording of human-confirmed reasoning.

## Immutability as calibration context

Type, layer, and kind are immutable after capture (baked into entry ID). Catch clear misclassification at capture time. Prose content (first sentences, summaries, descriptions) can be corrected after capture via supersede or mechanical edits. Cap findings about prose form at `medium`. Use `supersede` as the remedy for ambiguity rather than expanding the blocking surface.



## Graph fit against the grounding reading

Beside the checks above, judge draft against the reading on four more axes. Report each as its own category:

- `duplicates`: draft records what a live entry of the reading already records, so the capture adds a second entry for one claim instead of referencing or superseding the first.
- `outdated`: draft states as current what a live entry has since changed or retired. Or it leans on an entry whose status is superseded or closed without naming the entry that replaced or closed it.
- `strain`: two entries of the reading contradict each other on a point draft depends on. This is about the reading, not draft. Report it so the conflict can be resolved; never block draft for it alone.
- `dismissal`: an entry the author set aside bears on draft after all. Its reason code does not hold, and the entry belongs in the refs, or draft should supersede or close it. An entry implicitly dismissed counts as set aside: the author was shown it and left it unanswered.

Severity on these axes:

- `high`: draft outright duplicates a live entry, or states as current what a live entry retired, or an implicitly dismissed entry is a near-duplicate of draft.
- `medium`: the overlap or the staleness is partial, or a set-aside entry plainly bears on draft.
- `low`: a `strain` between two reading entries, and a weak overlap.

An entry that is only related to draft is no finding.

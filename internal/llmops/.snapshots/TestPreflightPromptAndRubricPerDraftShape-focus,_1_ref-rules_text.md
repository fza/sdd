## Task

A `kind: focus` decision is being captured: an involvement declaration with dual lifecycle (supersedable mid-cycle, completable via done signal). Mechanical frontmatter checks run separately at write time and block on errors (involvement list shape, target resolution, canonical actors, when format). **This rubric emits only low-severity, informational findings.**

## What this rubric does NOT enforce

Focus is fluid by nature: priorities shift, plans get superseded, work becomes unattributed mid-cycle. The supersession-vs-done-closure choice is exactly the lifecycle reasoning where pre-flight oscillates (per s-prc-vvd). This rubric:

- Does NOT argue whether closure rationale is "complete"
- Does NOT argue whether supersession scope is "appropriate"
- Does NOT argue whether involvement triples represent "enough commitment"
- Does NOT emit `high` or `medium` findings of any kind
- Does NOT enforce participation, prose density, or completeness norms
- Does NOT comment on layer choice: focus is layer-flexible like directive/plan/activity

## Observational findings (low severity only)

Three patterns are worth surfacing as observations. Frame them neutrally, to prompt user dialogue, not corrective action:

1. **Closure-without-target-completions**: focus is being closed by a done signal, but none of its involvement targets carry a done signal of their own. Wording: "this focus closes with no target completions". Could be intentional (the cycle ended without completing all targets; not a failure mode). User decides.

2. **Wholesale-priority-shift**: focus supersedes a prior focus and shares zero involvement targets with the predecessor. Wording: "this focus supersedes a focus with no shared targets — wholesale priority shift". Often intentional cleanup; surface for visibility.

3. **All-pull-available**: every involvement triple resolves to empty actors (either explicit `actors: []`, or focus-level `actors:` is also empty). Wording: "all involvement is pull-available". May be a deliberate "open for pickup" focus, or may indicate entry is incomplete. User decides.

If none of these apply, emit no finding for this rubric.

## Calibration

- `low` only: observational, neutral wording, never corrective
- No `high` or `medium` findings under any circumstances from this rubric

Do not introduce finding categories beyond the three calibrated above. Trust mechanical layer for structural validation. Frontmatter validation handles missing involvement, dangling targets, malformed when, and non-canonical actor names. Do not re-flag those here.

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

## Attachment claim consistency (apply to every entry)

Cross-check proposed entry's body against its `Attachments:` line. No such line means entry carries no attachment.

Flag only a body that asserts a record is attached **to this entry** ("the attached review", "attached as `notes.md`", "carries the full transcript as an attachment") while the `Attachments:` line is absent or names no matching file. Such an entry lies about what it carries: a later reader looks for the record and finds nothing.

This check is deliberately narrow. Emit no finding for:

- a body that only **mentions** a file, path, or document without claiming it is attached to this entry;
- a reference to an attachment **on another entry** (its ID appears with the mention);
- markdown links into attachment directory (`{{attachments}}/...`): a separate mechanical check owns those;
- an attachment the entry carries that body never mentions.

- `high`: body asserts an attached record, and entry carries no attachment (or none matching the assertion).
- No finding: everything else.

## Language check

No configured graph language (default English). No language-drift check applies.

## Active contracts (standing constraints: check for violations)

ID: 20260101-000007-d-cpt-con
Type: decision
Layer: conceptual
Kind: contract

The other contract body.

---

ID: 20260702-222259-d-cpt-7iy
Type: decision
Layer: conceptual
Kind: contract

The type-system contract body.

## Calibrating against active contracts

A finding against an active contract is `high` only when entry **directly contradicts** contract's core constraint: the spirit, not the letter. Subtle deviations, missing ritual markers, or wording that doesn't precisely echo contract are `medium` or `low`. Contracts express intent; treating them as literal rules invites find-something behavior. If entry's reasoning shows the agent considered contract and made a deliberate choice, that is `low` or no finding.

Test: would a reasonable reader say "this entry breaks the rule", or "this entry is in the spirit of the rule but worded differently"? Only the first is `high`.

## Dialogue context is grounding, not argument

Entry description may contain dialogue-sourced reasoning: references to discussions, trade-offs considered, alternatives rejected. Treat that reasoning as human-confirmed context. Focus on **structural completeness**: missing refs, uncovered requirements, missing evidence, type/layer mismatches, silent omissions. Do not re-evaluate rationale that was already dialogued. Do not argue with how a referenced entry is characterized. Do not demand rewording of human-confirmed reasoning.

## Immutability as calibration context

Type, layer, and kind are immutable after capture (baked into entry ID). Catch clear misclassification at capture time. Prose content (first sentences, summaries, descriptions) can be corrected after capture via supersede or mechanical edits. Cap findings about prose form at `medium`. Use `supersede` as the remedy for ambiguity rather than expanding the blocking surface.

## Task

A signal (observation/fact) is being captured. Perform a classification check.

## Checks

1. **Type correctness**: signals expand dialogue. They bring observations, potential directions, and solution-space thinking as material for discussion. Decisions resolve dialogue into commitments: they commit to a chosen direction, whether a one-line choice or a detailed plan. The test: does entry prescribe a commitment (should be a decision) or explore material for dialogue (signal)? A signal that describes possible solutions, proposes directions to investigate, or analyzes trade-offs is valid. It becomes a decision only when it commits to one.
2. **Layer appropriateness**: compare the proposed entry's `Layer` with the layer its body is about, by these meanings:

- strategic: why the work exists and where it heads (e.g. serve small teams first)
- conceptual: the approach, its core ideas and boundaries (e.g. entries are immutable)
- tactical: how a module or component behaves, its rules and structure (e.g. the parser rejects empty input)
- operational: one work step or task (e.g. rename a flag, bump a dependency, run a migration)
- process: how we work, conventions and review rules (e.g. every release gets a review)

   A rule or behaviour of one module or component, a gap in it included, belongs at `tactical`, not `operational`: a body stating how a component behaves is tactical even when one change carries it out. A single work step or task belongs at `operational`, not `tactical`: its body names the step to take. A specific implementation detail is never `strategic`; a broad directional observation is never `operational`. Decide it in two steps: name the layer the body's first sentence is about, then compare it with `Layer`. A first sentence that says what a named module or component does, accepts or rejects is `tactical` and never borderline. Report a mismatch as `medium`, quoting that first sentence and naming the layer the entry belongs at. A body that fits two layers equally is no finding.
3. **Confidence honesty**: the confidence level should be proportionate to the evidence. High confidence requires strong evidence; low confidence indicates uncertainty or hypothesis.

## Calibration

Signals are dialogue material. Focus on clear cases.

- `high`: imperative + timeline + ownership with no observational content, reading unambiguously as a committed decision (e.g. "We must migrate the database by next sprint. The team should start immediately with schema migration.").
- `medium`: observation present, but entry commits to a specific prescriptive design (e.g. "Drop-off is high at step 3. We must simplify the form by removing optional fields."). Worth raising whether the remedy belongs in a decision.
- `medium`: the layer clearly mismatches what the body is about (check 2).
- `low`: imperative opener with observational body.
- No finding: "Should either X or Y" offering alternatives, "should treat X as a valid outcome" proposing direction, or confidence author deliberately set. These are dialogue material.

Trust structural layer for type discipline. Kinds on signals (gap/fact/question/insight/done) are structural metadata and may evolve. Prose-level type policing belongs to kind system, not pre-flight.

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

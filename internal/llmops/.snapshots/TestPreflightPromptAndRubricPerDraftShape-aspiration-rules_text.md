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

## Calibrating against active contracts

A finding against an active contract is `high` only when entry **directly contradicts** contract's core constraint: the spirit, not the letter. Subtle deviations, missing ritual markers, or wording that doesn't precisely echo contract are `medium` or `low`. Contracts express intent; treating them as literal rules invites find-something behavior. If entry's reasoning shows the agent considered contract and made a deliberate choice, that is `low` or no finding.

Test: would a reasonable reader say "this entry breaks the rule", or "this entry is in the spirit of the rule but worded differently"? Only the first is `high`.

## Dialogue context is grounding, not argument

Entry description may contain dialogue-sourced reasoning: references to discussions, trade-offs considered, alternatives rejected. Treat that reasoning as human-confirmed context. Focus on **structural completeness**: missing refs, uncovered requirements, missing evidence, type/layer mismatches, silent omissions. Do not re-evaluate rationale that was already dialogued. Do not argue with how a referenced entry is characterized. Do not demand rewording of human-confirmed reasoning.

## Immutability as calibration context

Type, layer, and kind are immutable after capture (baked into entry ID). Catch clear misclassification at capture time. Prose content (first sentences, summaries, descriptions) can be corrected after capture via supersede or mechanical edits. Cap findings about prose form at `medium`. Use `supersede` as the remedy for ambiguity rather than expanding the blocking surface.

## Task

A `kind: aspiration` decision is being captured: a perpetual direction the graph pulls toward, with no completion criterion. Verify alignment with other active aspirations.

Aspirations form a constellation. Multiple active aspirations coexist and shape decisions across layers. Pre-flight checks whether this aspiration **contradicts an active aspiration** without acknowledging tension. Finding is gradient: aspirations are pulls, not constraints, so blocking would be the wrong shape. **Never `high`.**

## Checks

1. **Aspiration constellation alignment**: does narrative propose a direction that pulls against an active aspiration? If so, is tension acknowledged?
2. **Self-describing direction**: does aspiration name what it pulls toward in a way future decisions can align against?

## Calibration

- `medium`: narrative proposes a direction that contradicts an active aspiration without acknowledging tension. Example: an active aspiration pulls toward "dialogue shapes decisions", and new aspiration reads "ship v1 the fastest way possible" with no note on how the two coexist.
- `low`: aspiration is neutral or partially aligned with the active set; surface it for dialogue without blocking. Example: a new aspiration pulling toward "broad accessibility" while an active aspiration pulls toward "deep specialist tooling". Related axes, some tension, but no direct contradiction.
- No finding: aspiration moves toward an active aspiration (reinforcement), explores an orthogonal axis (e.g., technical excellence vs. user accessibility as distinct pulls), or narrative explicitly acknowledges tension with another aspiration and argues why both can coexist.
- **Never `high`**: aspirations are gradient. If an aspiration looks genuinely incompatible, that's `medium` and a dialogue prompt, not a block.

## Active aspirations (current constellation: check for tensions)

ID: 20260101-000006-d-stg-asp
Type: decision
Layer: strategic
Kind: aspiration

The aspiration body.

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

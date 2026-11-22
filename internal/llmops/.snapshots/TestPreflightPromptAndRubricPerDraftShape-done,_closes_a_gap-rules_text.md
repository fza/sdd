## Artifact durability (apply only to `kind: done` signals)

This check applies **only** when proposed entry is a `kind: done` signal recording completed work. For any other entry (decisions, non-done signals), emit no finding from this section.

A done signal's claim must rest on a durable, checkable artifact fit for the act it records. Any of these is a durable artifact:

- a **commit** in the Git-tracked repository;
- an **attachment on this entry** (`--attach`): the durable form when the deliverable *is* the document (a research, synthesis, evaluation, or design write-up);
- an **attachment on a referenced upstream entry** already in graph;
- a **URL** to work whose result lives outside this repo (a merged PR/MR, a CI run, a deployed page, an external document);
- for an act that **changed nothing** (a manual or behavioral verification, e.g. "confirmed X still works in a fresh session"): a **human participant's attestation in the body** is itself the durable trace.

Judge the act from the narrative, then cite the **most specific** artifact it produced. "Most specific" is guidance, not a rule. Accept any durable artifact that genuinely corresponds to the act. Never flag a valid attachment, URL, or attestation only because a more specific form could also exist.

The single hard requirement is for **changes to the repository's code or source**: editing or adding code, templates, configs, or other tracked source. Such a change must cite the **commit** that landed it. Commit is the durable, in-band artifact; a prose description or commentary notes do not replace it. This requirement does **not** extend to a done whose deliverable *is* an attached document (research/synthesis/evaluation/design). There the attachment is the artifact, and no commit is expected.

- `high`: a done that records a code/source change to the repository but cites no commit. Description alone is not evidence. Commentary notes attached to entry do not substitute for commit that landed the change.
- `medium`: a done whose act produced a durable artifact living **outside** this repo (a deploy, a release, an external change) but cites none: no URL, no attachment. Surface the missing pointer. This is advisory, not a hard block.
- `low`: a durable artifact exists, but entry doesn't name it explicitly. Or a less-specific artifact is cited where a tighter one was readily available.
- No finding: entry cites a durable artifact fit for its act (a commit, an attachment on this entry or an upstream entry, or a URL); **or** the act changed nothing and a human participant attests the result in body; or proposed entry is not a `kind: done` signal.

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

A `kind: done` signal is being created that closes a `kind: gap` signal directly, bypassing an intermediate decision. Verify that this short-loop closure fits gap's layer and done entry's narrative scope.

## Checks

1. **Gap coverage**: done's narrative addresses what gap identified. It must resolve specific concern raised, not a neighboring one.
2. **Short-loop appropriateness**: at this layer, with this narrative scope-shape, is bypassing a decision appropriate? Short-loop is valid for low-layer execution fixes. Higher layers and approach-shaped narratives deserve a captured decision.
3. **No smuggled decision**: narrative must not describe a choice that would have warranted a decision (e.g. a reframing, a trade-off being made, an approach being selected).
4. **Artifact durability**: apply "Artifact durability" above as a check of its own. The scope-shape matrix below does not cover it.

## Narrative scope-shape

- **Execution-shaped**: narrative describes what was done (*"updated X to prevent Y"*, *"removed redundant check so Z"*)
- **Approach-shaped**: narrative describes a choice or justification (*"changed the approach to Z because W"*, *"reframed X as Y"*)

## Calibration

Calibration runs on the matrix below: layer of closed gap × scope-shape of done narrative.

| Layer of gap | Execution-shaped done | Approach-shaped done |
|---|---|---|
| stg | `high` | `high` |
| cpt | `medium` | `high` |
| tac | no finding | `high` |
| ops / prc | no finding | `medium` |

Bands:

- `high`:
  - Gap at stg closed by done, whatever the scope-shape: strategic gaps require a captured decision
  - Gap at cpt or tac closed by approach-shaped done: the choice is smuggled into done; it should have been a decision
  - Done does not address gap it claims to close (coverage failure)
  - Example: gap "pre-flight over-rejects on wording" closed by done "Changed pre-flight to allow solution-space thinking because signals explore, decisions commit". Approach-shaped at cpt; should have been a decision capturing the reframing

- `medium`:
  - Gap at cpt with execution-shaped done: conceptual scope, no explicit choice in prose, but worth surfacing "reads like it could be a decision?"
  - Gap at ops / prc with approach-shaped done: low-layer choice worth capturing as a decision; surface without blocking
  - Example: gap "session startup is slow" closed by done "Reduced synthesis to recent entries because full depth was slow". Approach-shaped at ops, medium surface

- `low`:
  - Done technically addresses gap but could be more specific about which aspect
  - Example: gap "pre-flight over-rejects on wording" closed by done "Improved pre-flight prompt". General fix without naming specific aspect

- No finding:
  - Gap at ops / prc with execution-shaped done describing pure execution
  - Standard short-loop fix at low layers: observation, then fix
  - Example: gap "catch-up cites superseded decision" closed by done "Updated skill example to reference current decision"
  - Deferral explicitly acknowledged ("minimal fix; deeper rework deferred"): trust author's dialogue-grounded judgment

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

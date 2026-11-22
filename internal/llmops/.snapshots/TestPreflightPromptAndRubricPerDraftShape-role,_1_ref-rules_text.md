## Unrelated references check

`refs` represent a "builds on / depends on" relationship: entry's reasoning should be grounded in referenced entries. Flag refs that don't topically connect to proposed entry's concern and whose "builds on" relationship isn't evident from narrative.

- `high`: ref points at an entry with no topical connection to this entry's concern, AND narrative does not explain the link. Example: a plan about CSV parsing refs a WIP-marker-improvement signal. Narrative never mentions coordination or WIP marker behavior, so ref looks accidental or cargo-culted.
- `medium`: ref is tangentially related, but the "builds on" relationship isn't evident. Referenced entry is in the same area, but proposed entry doesn't explain how it depends on or builds from ref.
- `low`: ref is related, but a one-sentence note would sharpen the connection for future readers.
- No finding: proposed entry has no refs, OR refs are upstream context the entry genuinely builds on (e.g., plan operationalizes a ref'd directive; signal observes against a ref'd contract). Conceptual grounding (e.g., a decision refs a contract it honors) needs no connective sentence: the link is structural.

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

## Reference metadata consistency (apply when the entry has refs)

A ref carries a `kind` — why the pointer exists, chosen from what the entry **body** asserts — and an optional `desc` naming that relationship. Both must read consistently with the body.

To judge whether a ref's kind or desc fits the body, open the ref-kind fact 20260828-160000-s-prc-rfk by its ID: it defines each kind, its direction and its neighbouring kinds.

Two axes remain yours — both read the **body**, which the mechanical check cannot:

- **Kind-vs-body** — among the admissible kinds, does the chosen one name the relationship the body actually asserts, or does the body's own wording indicate a different admissible kind?
- **Desc-vs-body** — what the desc claims about the relationship vs. what the body actually does with the reference. Skip this axis when no ref carries a `desc`.

## Calibration

- `high` — *Desc contradiction only*: the body affirmatively refutes the relationship the desc claims (e.g. `desc: "extends the predecessor contract"` while the body says "this directive retires that contract"). Name the ref ID in the observation. The finding's `quote` is the body sentence that refutes the desc, never the desc's own words: the desc is what the author rewrites, the body sentence is the evidence. A desc the body flatly refutes is `high`, never `medium`. A kind question is **never** `high`.

- `medium` — Two cases, each requiring quotable evidence:
  - *Evidence-backed kind mismatch*: the body's own wording indicates a different **admissible** kind — quote the body phrase in your observation (e.g. body says the work is "gated on X landing first" while the ref says `related`; `depends-on` is the admissible kind that phrase names, and the quote is the evidence). No quotable body anchor → this is `low`, not `medium`.
  - *Genuinely misleading desc divergence* without flat contradiction: a future reader would reasonably read the wrong relationship from the desc alone (e.g. a `desc` naming a different basis than the body argues from). Reserve for real desc misreads.

- `low` — Defensible but sharper, with no body phrase to quote: the chosen kind reads fine, yet another admissible kind might name the body slightly more precisely (e.g. `builds-on` vs `grounded-in` on a terminal `done` — a documented author's-call tie-break, never worth more than `low`). Also mild desc focal-length differences. The augmenting pattern (a directive that `refines` an active plan it sharpens in place) is correct, not a finding.

- No finding — the kind and desc read correctly against the body, or no ref carries a `desc` and every kind reads correctly.

The body is the source of truth; the kind is the structural commitment downstream consumers (heat ranking, render expansion) key on, so getting the kind right matters more than the prose-level desc. `related` is the floor — flag it only when the body itself names a sharper admissible relationship (then quote it, `medium`) or reads as a plain mistagging of a desc-stated relationship.

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

A `kind: role` decision is being captured: a participation pattern binding an actor to a specific contribution shape. Mechanical frontmatter checks run separately (actor canonical matches an active chain's head, refs includes the head actor entry ID). Focus this rubric on **contribution-shape prose quality**.

## Role rubric

Roles record *what a participant does*, orthogonal to strategic or conceptual contracts. Description prose should make the contribution shape legible.

1. **Contribution scope**: what kind of work does this role cover? Review, authorship, domain expertise, decision weight. Prose should name the shape clearly enough that another participant can reason about when to route work or dialogue to this actor.
2. **Orthogonality to contract**: roles are *per-actor* participation patterns, not *universal* rules. If prose describes what "everyone" should do, it belongs in a contract. Role prose names one actor's contribution.
3. **Actor linkage**: prose should reference the bound actor canonical (also present in the `actor:` frontmatter field), so role reads coherently without frontmatter visible.

## Calibration

- `high`: prose describes a universal rule or team-wide discipline rather than one actor's participation shape. Belongs in a contract, not a role.
- `medium`: contribution shape is named, but prose is vague ("Christopher contributes to the project"): not enough shape to route work against.
- `low`: prose omits actor linkage. Frontmatter carries it, but the reader has to cross-reference.
- No finding: contribution shape is clear, actor linkage is named, scope is per-actor rather than universal.

Trust mechanical layer for structural validation. Frontmatter validation, pre-flight mechanical checks, and lint handle actor canonical mismatch, missing head ref, orphan cascade, and layer mismatches. Do not re-flag those here.

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

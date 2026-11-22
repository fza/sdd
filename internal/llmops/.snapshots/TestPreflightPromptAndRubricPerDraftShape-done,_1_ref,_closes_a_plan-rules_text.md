## Unrelated references check

`refs` represent a "builds on / depends on" relationship: entry's reasoning should be grounded in referenced entries. Flag refs that don't topically connect to proposed entry's concern and whose "builds on" relationship isn't evident from narrative.

- `high`: ref points at an entry with no topical connection to this entry's concern, AND narrative does not explain the link. Example: a plan about CSV parsing refs a WIP-marker-improvement signal. Narrative never mentions coordination or WIP marker behavior, so ref looks accidental or cargo-culted.
- `medium`: ref is tangentially related, but the "builds on" relationship isn't evident. Referenced entry is in the same area, but proposed entry doesn't explain how it depends on or builds from ref.
- `low`: ref is related, but a one-sentence note would sharpen the connection for future readers.
- No finding: proposed entry has no refs, OR refs are upstream context the entry genuinely builds on (e.g., plan operationalizes a ref'd directive; signal observes against a ref'd contract). Conceptual grounding (e.g., a decision refs a contract it honors) needs no connective sentence: the link is structural.

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

A completion entry (a `kind: done` signal) is being created that claims to close (resolve/fulfill) a decision. Verify that closing entry genuinely covers everything the decision requires.

## Checks

1. **Coverage**: closing entry's description must account for every requirement or plan item in closed entry: either implemented or explicitly deviated from.
2. **No silent omissions**: a plan item that is neither implemented nor mentioned is a gap.
3. **Explicit deviations are acceptable**: if closing entry marks an item as an intentional deviation with reasoning (e.g. "Deviation: no X, decided during implementation that Y because Z"), that counts as coverage.
4. **No overclaiming**: closing entry must not claim to have done things beyond what its description supports.
5. **Acceptance criteria coverage** (applies only when closed entry is `Kind: plan`): plan decisions carry a `## Acceptance criteria` section in their description. Closing entry must address each AC item: confirmed done with specific evidence, or deviation explained with dialogue reasoning. If closed entry's kind is not `plan`, skip this check.

## Calibration

The work lives in commits, attachments, and upstream entries, not necessarily restated in closing entry's prose.

- `high`: closing entry cites evidence that contradicts its own claim (e.g. citing a reference that refutes the closure). Or plan has a specific requirement (e.g. "test moved or added in the correct package"), and closing entry explicitly contradicts it without explanation.
- `medium`: a plan item has specific behavior, and closing prose leaves it ambiguous (e.g. plan says "no automatic cleanup; agent handles removal manually"; closing entry mentions cleanup only at a high level).
- `low`: deferral is acknowledged in text without an explicit "Deviation: ..." marker format.
- No finding: implementation details that live in committed code. Standard "per d-tac-X plan" opening for closing entries. Format specs whose source of truth is plan attachment or code.

### AC coverage calibration (when closing a plan)

**Core rule**: every AC must either be implemented (with evidence) or carry reasoning for deviation/omission. Pre-flight checks that reasoning is **present**, not whether it is valid. Validity is author's dialogue-grounded judgment, already confirmed at capture time.

- `high`: an AC is not implemented AND no reasoning is present. This covers silent omission (AC never mentioned) and explicit non-implementation without why (e.g. "Items 2 and 4 were not addressed."). Both fail the core rule equally.
- `medium`: AC coverage is **ambiguous**. Closing entry mentions something related, but specific required behavior isn't evident. Plausibly implemented; worth clarifying. Example: AC "sdd show renders depth 0 full, depth 1+ as summary lines"; closing entry "Depth-limited rendering implemented."
- `low`: AC addressed, but phrasing thin (e.g. AC: "--max-depth flag with default 4"; closing entry: "Added --max-depth flag.", missing the default value).
- No finding: AC confirmed done with specific evidence, OR any form of deviation/omission that carries reasoning (however brief). Do not judge whether reasoning is convincing: its presence is enough. Example: "Deviation: single --max-depth instead of per-direction, decided during dialogue that independent depths add complexity without corresponding benefit."

Don't demand prose restatement of work that lives in the code or attachments.

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

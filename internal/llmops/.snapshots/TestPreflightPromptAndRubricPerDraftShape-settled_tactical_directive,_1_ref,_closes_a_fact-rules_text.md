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

## Settled-directive justification (apply only to a directive with intent: settled)

This check applies **only** when proposed entry is a `kind: directive` decision carrying `Intent: settled` (shown beneath proposed decision). If proposed entry is any other type, any other decision kind, or a directive whose intent is `pending`, `guiding`, or unspecified, skip this section entirely and emit nothing for it.

A `settled` directive is **born terminal**. It commits to something that needs no follow-up and carries no closing edge. It never enters the normal "open until a done signal closes it" lifecycle. So body must say *why* it is complete on capture: what was decided, and a reason no further action follows (e.g. "keep X as-is: Y never reaches its limit, so nothing needs building", "no action needed because Y", an intake item dismissed with its reason). Without that reason, a reader cannot tell a deliberate born-terminal decision from one that dropped its follow-up.

A choice alone is not a reason, even one phrased as keeping or using what exists. "Use the existing retry helper for the upload client." names a choice and states no reason nothing follows: it draws the finding. Look for a sentence that gives the why; a body with none is unjustified however settled its choice sounds.

### Rubric

- `medium` `settled-unjustified`: directive is `intent: settled`, but its body states no reason the decision needs no follow-up. Name what's missing.
- No finding: body states the decision and a reason nothing further is required (a question put to rest with its answer, a deliberate "keep as-is" with why it stays, an intake dismissal with rationale).

Severity is at most `medium`: an unjustified settled directive is worth surfacing for dialogue, not blocking. Emit at most one `settled-unjustified` finding.

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

A decision is being created that claims to close one or more entries. Most often this is a decision closing signals (the standard pattern: directive/activity/plan/contract addressing a gap). A `kind: directive` decision may also close stable-kind entries (`fact` / `insight` / `contract` / `aspiration`) as a retirement. Verify that decision genuinely addresses each closed entry, and that retirement cases carry rationale.

## Checks

1. **Genuine resolution**: decision content must genuinely address each closed signal, not only reference it as context.
2. **No silent additions**: decision must not introduce commitments not grounded in referenced signals. Strategic synthesis is allowed; unrelated scope is not.
3. **Layer appropriateness**: compare the proposed entry's `Layer` with the layer its body is about, by these meanings:

- strategic: why the work exists and where it heads (e.g. serve small teams first)
- conceptual: the approach, its core ideas and boundaries (e.g. entries are immutable)
- tactical: how a module or component behaves, its rules and structure (e.g. the parser rejects empty input)
- operational: one work step or task (e.g. rename a flag, bump a dependency, run a migration)
- process: how we work, conventions and review rules (e.g. every release gets a review)

   A rule or behaviour of one module or component, a gap in it included, belongs at `tactical`, not `operational`: a body stating how a component behaves is tactical even when one change carries it out. A single work step or task belongs at `operational`, not `tactical`: its body names the step to take. A specific implementation detail is never `strategic`; a broad directional observation is never `operational`. Decide it in two steps: name the layer the body's first sentence is about, then compare it with `Layer`. A first sentence that says what a named module or component does, accepts or rejects is `tactical` and never borderline. Report a mismatch as `medium`, quoting that first sentence and naming the layer the entry belongs at. A body that fits two layers equally is no finding.
4. **Retirement rationale** (applies only when closed target's kind is `fact`, `insight`, `contract`, or `aspiration`): decision narrative must state **why** target is being retired. Check only that the reason is present, not whether it is correct.
5. **Settled justification** (applies only to a directive with `Intent: settled`): apply "Settled-directive justification" above as a check of its own.

## Calibration

A decision closing a signal must address what signal identifies. Apply contract calibration.

- `high`: decision claims to close a signal with a specific quantitative target but misses it without justification (e.g. signal requires ≤30s for default catch-up; decision delivers ~1m and does not explain the shortfall).
- `medium`: decision addresses most of signal; one concern is silent without acknowledgment (possible intentional scope narrowing, worth naming).
- `medium`: the layer clearly mismatches what the body is about (check 3).
- `medium` `settled-unjustified`: a settled directive whose body states no reason it needs no follow-up (check 5).
- `low`: rationale is present but thin; a sentence explaining why would help future readers.
- No finding: a plan omits a structural ritual (e.g. explicit CQRS decomposition table) required by an active contract. Contracts express intent, not rigid form; small tactical plans don't need decomposition tables. A decision introduces synthesized design direction (e.g. "agent-neutral abstraction") beyond what signal literally requested. Decisions synthesize: the grounding test is "does the signal motivate a response?", not "is the exact direction pre-established?".

### Retirement-rationale calibration (when closing a stable-kind entry)

Stable-kind entries (`fact`, `insight`, `contract`, `aspiration`) have nothing intrinsic to resolve: they're retired, not fulfilled. Pre-flight checks that narrative states **what changed** to warrant retirement. It does not judge whether reasoning is compelling.

- `high`: retirement claim with no why-retired statement. Example: closing a contract with narrative "No longer relevant." and nothing more; closing an aspiration with only "Retiring this."
- `medium`: reasoning restates what was closed without naming what changed. Example: "Retiring the immutability contract. It said documents don't change." (recaps contract but never explains what shifted to make retirement appropriate).
- `low`: reasoning present but thin; a sentence would sharpen it. Example: "New evidence made this outdated." Directionally correct, but specific evidence or shift isn't named.
- No finding: narrative cites a specific reason: new evidence with specifics, a scope change with what changed, an explicit handoff to a new entry ("superseded effectively by the aspiration `d-cpt-xyz`"), a concrete context shift, or a reclassification ("reclassifying this directive as a contract"). The reason's *correctness* is not evaluated; its *presence* is.

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

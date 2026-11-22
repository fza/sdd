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

A ref carries a `kind` — why the pointer exists, chosen from what the entry **body** asserts — and an optional `desc` naming that relationship. Both must read consistently with the body. The canonical ref-kind vocabulary follows, rendered from the graph's own ref-kind fact — the same single source agents pull; judge the chosen kind and desc against it.

### Connecting entries: the reference kinds

Reference is a contextual pointer from one entry to another. Its kind names why the pointer exists. A ref has no status effect: closure travels in `closes` and `supersedes`, never in a ref kind. Write the body first, then tag the strongest relationship it states. Kind is chosen from what the body asserts, never defaulted. Kinds are defined by relationship and direction, not by allowed target types. Whether a target is empirical (a fact, a done) or conceptual (an insight, a decision) is read from the target's kind.

**Direction.** Ref is written on the source, pointing at the target: read as `source → target`. Most kinds point backward at prior context. Small forward class, `surfaces` and `required-by`, is captured on the source pointing at what comes out of it. Entries are immutable and can only reference what already exists. So kind follows capture order: whichever entry is written second carries the edge. Two relationships are order-independent pairs: `depends-on`/`required-by` and `surfaces`/`surfaced-by`. Second-captured entry takes the forward kind (`required-by`, `surfaces`) when it is the prerequisite or producer. It takes the backward kind (`depends-on`, `surfaced-by`) when it is the dependent or product. One ref connects both ways: target's downstream view shows the source. So a paired ref needs no back-ref. Never add a weak `related` just to point back.

| Kind | `source → target` | Apply when | Not when |
|---|---|---|---|
| `grounded-in` | founded on / reasons from | the target is a basis the source rests on — a contract, aspiration, standing directive, a **fact** taken as premise or cited as empirical proof, an **insight** reasoned from, or a prior decision conformed to | you realize/operationalize it → `addresses`; you extend a closed line → `builds-on` |
| `builds-on` | continues / extends | the target is **closed** and you extend it, or you are the next step after a finished chain | the target is active and sharpened in place → `refines`; you realize its commitment → `addresses` |
| `refines` | sharpens (in place) | the target is **active** and you narrow/clarify its commitments without replacing it (the augmenting pattern; the refining entry closes alongside the target) | the target is closed → `builds-on`; you realize it → `addresses` |
| `addresses` | acts on / realizes | responding to a gap/question/insight, **or** realizing a decision's commitment — operationalizing a directive, supplying a plan's AC or an activity's work (including partial, without closing) | you only reason from it → `grounded-in`; the target depends on you → `required-by`; the target is a terminal `done` → `builds-on`/`grounded-in`/`surfaced-by` |
| `surfaces` | created / discovered (forward) | doing the source's work created or discovered the target; capture the surfaced entry first, then the source that refs it | generic neighborly context → `related` |
| `surfaced-by` | was raised / produced by (backward) | the target's work created or discovered this entry, captured after the surfacer — the backward partner of `surfaces`, including when the surfacer is a terminal `done` | you only **reason from** the target → `grounded-in`; generic neighborly context → `related` |
| `depends-on` | needs first (prerequisite) | your work is gated on the target landing or holding first | the target is a basis you reason from → `grounded-in` |
| `required-by` | is the prerequisite of (forward) | this entry is what a later plan/activity/focus was waiting on, recorded from the prerequisite's side | you do/supply the target's work rather than gate it → `addresses` |
| `related` | sibling / neighbor (the floor) | a genuine parallel sibling, or context a decision must account for but does not fulfil — when no sharper kind fits | any sharper kind fits — check the others **first** |

**`grounded-in` absorbs empirical citation.** Citing a fact's or a done's measured data as proof is `grounded-in` to that entry. Empirical hardness is read from the target's kind. Put the proof nuance in the `desc` ("… as evidence of feasibility").

**`refines` vs `builds-on`** turns on the target's status. Active and sharpened in place: `refines`. Closed, or a forward next step: `builds-on`.

**`related` is the floor, never a default**: the most over-reached kind. For a decision target, split. Source realizes it: `addresses`. Source is context it accounts for but does not fulfil: `related`.

**A terminal `done` is never "addressed".** Done records completed work: not a gap, question, insight or open commitment. So the relationship `addresses` names cannot hold for it. Taking up a follow-up a done flagged: `builds-on`. Reasoning from the done as empirical evidence: `grounded-in`. Entry the done's work raised or produced: `surfaced-by`. Choosing between `builds-on` and `grounded-in` here is a defensible-choice question, not an error.

**`surfaced-by` vs `grounded-in`.** Use `surfaced-by` when the target's work raised or produced this entry: it exists because of that work. Use `grounded-in` when this entry merely reasons from the target as a basis. Split matters most when the surfacer is a terminal done. `addresses` is blocked there. Without `surfaced-by`, the raised-by relationship gets mis-anchored onto a convenient live decision.

#### Mechanics

The ref kinds are a closed set: grounded-in, builds-on, refines, addresses, surfaces, surfaced-by, depends-on, required-by, related.

Enforced at capture: every reference carries one of these kinds, and every target must resolve or capture is blocked. Legacy values on disk (grounds, evidence) read as grounded-in and are not capturable.

**Applicability is not your question.** Each referenced entry carries an `Admissible ref kinds for this target` line and a `Chosen ref kind` verdict, both computed mechanically from the target's kind and derived status. An inapplicable chosen kind is already blocked by a separate mechanical check before your findings are read. Never emit a finding about whether a kind applies, and never recommend a kind outside the listed admissible set.

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

A decision is being created with references but not closing any entries. Verify that decision is well-grounded and complete. For directive-kind decisions, also verify that it reads as a bounded commitment, not a perpetual pull (which would be an aspiration).

A common shape under this check is the **augmenting directive** (see "Augmenting directives" below). AC-refinement-via-directive is a first-class resolution path alongside supersession. Do not reflexively propose supersede or amend when proposed entry matches the augmenting-directive shape.

## Augmenting directives

An augmenting directive is a `kind: directive` decision that refs an active plan, has no `supersedes` and no `closes` on plan, and refines or clarifies one of plan's acceptance criteria. Plan stays immutable; directive joins plan's implicit AC chain. Closing happens when plan's done signal lists both plan and the augmenting directive in `--closes`.

This is a named pattern, not a deviation. When proposed entry matches this shape:

- **AC tension is resolved by refinement, not supersession.** Directive sharpens, narrows, or clarifies plan's AC contract while plan stays untouched. On this basis, do not propose plan amendment, AC supersession, or directive weakening as findings. AC-refinement-via-directive is a permitted resolution path.
- **Refs are one-way.** Directive refs plan; plan does not back-ref directive. Visibility runs through `sdd show <plan-id> --downstream`. Do not ask for ref'd plan to be amended with a backward ref.
- **Lifecycle is split.** Directive stays open across plan's implementation window. Plan's done signal closes it alongside plan. Do not flag this as dangling commitment or closure-sequencing ambiguity.
- **Narrowing or sharpening AC scope is the pattern's purpose.** It is not "scope smuggling". That check catches undiscussed commitments outside ref'd entries' support, not refinements ref'd plan itself invites.
- **Graph-mechanics meta-patterns are not refs.** Names in prose that identify entry's shape (e.g. citing the augment-plan pattern itself) are framework-level rules, not logical dependencies for this entry. Do not ask for them as refs.

Genuine supersession is still distinct from augmentation. A directive that *replaces* a core AC commitment (asserting AC is wrong, not narrower or sharper) belongs in a supersedes operation on plan. Augmentation refines; supersession replaces.

## Checks

1. **Ref completeness**: read body against referenced entries below. Every entry the body reasons from, responds to, contrasts with, or advances should appear as a ref. If a sentence needs another entry's context to make sense, that entry should be referenced. Judge this from body itself and referenced entries below. Do not hunt the wider graph for entries that might be related: skill-side widening before capture covers that. Refs represent logical dependency ("builds on"), not topical association. (Graph-mechanics meta-patterns named in prose are not refs; see "Augmenting directives" above.)
2. **Grounding**: decision content should be grounded in referenced entries, not introduce commitments from nowhere.
3. **No scope smuggling**: decision should not contain undiscussed commitments beyond what referenced entries support. (Refining the scope of a ref'd active plan via the augmenting-directive pattern is not scope smuggling; see "Augmenting directives" above.)
4. **Layer appropriateness**: compare the proposed entry's `Layer` with the layer its body is about, by these meanings:

- strategic: why the work exists and where it heads (e.g. serve small teams first)
- conceptual: the approach, its core ideas and boundaries (e.g. entries are immutable)
- tactical: how a module or component behaves, its rules and structure (e.g. the parser rejects empty input)
- operational: one work step or task (e.g. rename a flag, bump a dependency, run a migration)
- process: how we work, conventions and review rules (e.g. every release gets a review)

   A rule or behaviour of one module or component, a gap in it included, belongs at `tactical`, not `operational`: a body stating how a component behaves is tactical even when one change carries it out. A single work step or task belongs at `operational`, not `tactical`: its body names the step to take. A specific implementation detail is never `strategic`; a broad directional observation is never `operational`. Decide it in two steps: name the layer the body's first sentence is about, then compare it with `Layer`. A first sentence that says what a named module or component does, accepts or rejects is `tactical` and never borderline. Report a mismatch as `medium`, quoting that first sentence and naming the layer the entry belongs at. A body that fits two layers equally is no finding.
5. **Acceptance criteria presence** (applies only when proposed entry is `Kind: plan`): plan decisions must include an `## Acceptance criteria` section in description with `- [ ]` checklist items. A missing AC section on a plan decision is a `high` finding. If proposed kind is not `plan`, skip this check.

## Calibration

Judge ref completeness from body and referenced entries below, not from a graph-wide search. A decision must ref the entries its body reasons from, responds to, or advances. Not every tangentially-related entry needs a ref.

- `high`: proposed entry is a plan decision with no `## Acceptance criteria` section in its description.
- `medium`: body names a specific entry (by ID or unmistakable description) it depends on, but that entry is absent from refs. Or reasoning clearly leans on context no referenced entry supplies, suggesting a motivating entry is missing. Could be intentional scope narrowing.
- `medium`: the layer clearly mismatches what the body is about (check 4).
- `low`: style suggestion (dense summary could be split across sentences).
- No finding: referenced entries adequately ground body and no other check fails. Strategic decisions synthesize direction without every principle being pre-established in a signal; demanding pre-grounding prevents strategic thinking. Presence-of-field checks (missing Kind, missing Confidence) belong to CLI/lint, not LLM judgment.

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

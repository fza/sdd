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

A `kind: actor` signal is being captured: a first-class participant identity entry. Mechanical frontmatter checks run separately (canonical presence, canonical write-once invariant). Focus this rubric on **prose quality**.

## Actor rubric

Actor signals record *who* a participant is. Description prose should make identity legible to future readers without external context.

**Identity vs role is frame-relative.** An actor's role *inside this graph* (how they contribute to the project whose graph this is) belongs in a `kind: role` decision, not actor signal. From this graph's view, these are **identity content**: organizational positions held in other frames (CEO of another company, employed at X, member of Y), career background (senior developer, product manager, academic), prior domain expertise, and other external or historical attributes. They ground *who participant is* and belong in actor prose. The test: does prose describe what participant *does here* (a role), or what they *bring* from outside this graph (identity)?

1. **Canonical introduction**: prose should introduce canonical as the preferred identity string used in `participants` fields across graph. One sentence is enough: "Christopher, canonical `Christopher` in graph participants, is CEO of networkteam with a background as full-stack developer and technical lead."
2. **External context (optional)**: a sentence or two of role-independent context helps future readers disambiguate and grounds identity. Suitable content: affiliation with external organizations (employer, institution), career background, prior domain expertise, provenance participant brings from outside this graph. Missing external context is acceptable. Conflating this-graph role scope with actor prose is a finding.
3. **Alias explanation (when aliases are present)**: if `aliases:` lists variants, prose should name the dialogue/mining contexts those variants appear in. Agents then know when to resolve them on the read side (aliases are never used at capture time).

## Identity scope for non-individual actors

Some actors represent a *family* or *collective*, not a specific individual or session. Examples: an AI model family where individual sessions vary by version and harness, or a team whose membership shifts over time. Prefer the stable identity (the family, the team) over the momentary instance (a specific session, a current member roster). Model-family-as-identity is the correct framing for AI actors. Specific-session detail belongs in narrative, not in identity record.

## Calibration

- `high`: prose reads as a role description (what participant does *inside this graph*) rather than an identity record (e.g. "Christopher reviews PRs and owns migrations", "Claude drafts entries and executes CLI operations"). That belongs in a role decision, not actor signal.
- `medium`: canonical is present in frontmatter, but prose never names or introduces it. Entry is an identity record with no readable identity.
- `low`: prose is terse to the point of being uninformative ("actor: Christopher"), but frontmatter canonical and any context prose are intact.
- No finding: prose grounds identity, names canonical, and keeps this-graph role scope out.

Do not introduce finding categories beyond those calibrated above. External organizational roles, career background, and identity scope (family vs session, collective vs individual) are identity content, not findings. Trust mechanical layer for structural validation. Frontmatter validation and the lint invariant check handle missing canonical, cross-chain canonical reuse, and layer mismatches. Do not re-flag those here.

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

# Composition of the deep verifier's "Pre-flight's rules" pages

Branch `fza-next` at commit `0a89bcb5`, the graph of this repository (1,889 entries with the embedded base entries, no load issue), 2026-11-22. All sizes are bytes.

## Method

The sizes come from calling the exported function, not from counting template files.

- A program outside the committed tree loads the graph and calls `llmops.PreflightRubric(draft, graph, "")` (`internal/llmops/preflight.go:149`) with synthetic drafts. Each draft has the body `Draft body.`, and its refs and closes point at real entries. The language argument is empty because the project configures no language.
- The program splits each rules text at level-2 headings and keeps the contracts block whole. Pages come from `model.DeepContinuedPages` with the two openings of `preflightRules` (`pkg/application/workflow_deep.go:761-767`).
- The draft, its referenced, closed and superseded entries are not in the rules text (`internal/llmops/preflight.go:158-162`, `:187-195`). The size therefore depends only on the draft's shape (kind, layer, intent, has refs, has closes, kinds closed) and on the graph (contracts, aspirations, the ref-kind fact).

## 1. Parts of the rules text, four shapes

| Part | Gap signal, no refs | Gap signal, 1 ref | Plan, 1 ref | Directive (tactical), 1 ref |
|---|---:|---:|---:|---:|
| **System prompt** | **16,915** | **27,328** | **27,328** | **27,328** |
| Unrelated references check | 0 | 1,193 | 1,193 | 1,193 |
| Attachment claim consistency | 1,063 | 1,063 | 1,063 | 1,063 |
| Reference metadata consistency, rules | 0 | 7,033 | 7,033 | 7,033 |
| of which the ref-kind vocabulary | 0 | 5,719 | 5,719 | 5,719 |
| Reference metadata consistency, calibration | 0 | 2,187 | 2,187 | 2,187 |
| Language check (no language configured) | 101 | 101 | 101 | 101 |
| Active contracts (8 entries) | 14,134 | 14,134 | 14,134 | 14,134 |
| Calibrating against active contracts | 668 | 668 | 668 | 668 |
| Dialogue context is grounding | 534 | 534 | 534 | 534 |
| Immutability as calibration context | 415 | 415 | 415 | 415 |
| **Selected check** | **3,266** | **3,266** | **6,206** | **5,794** |
| check name | signal-capture | signal-capture | decision-refs | decision-refs |
| rules | 2,215 | 2,215 | 5,101 | 4,794 |
| calibration | 1,051 | 1,051 | 1,105 | 1,000 |
| **Graph-fit axes** | **1,418** | **1,418** | **1,418** | **1,418** |
| **Total** | **21,599** | **32,012** | **34,952** | **34,540** |
| Contracts' share | 65% | 44% | 40% | 41% |
| Pages | 2 | 3 | 4 | 4 |

Further shapes:

| Shape | Check | System | Check | Axes | Total | Pages |
|---|---|---:|---:|---:|---:|---:|
| Plan, 1 ref, closes a gap | closing-decision | 29,402 | 3,229 | 1,418 | 34,049 | 3 |
| Directive (conceptual), 1 ref | decision-refs | 27,328 | 7,524 | 1,418 | 36,270 | 4 |
| Directive (tactical, settled), 1 ref | decision-refs | 29,152 | 6,061 | 1,418 | 36,631 | 4 |
| Directive (conceptual, settled), 1 ref | decision-refs | 29,152 | 7,791 | 1,418 | 38,361 | 4 |
| Directive (tactical), no refs | decision-refs | 16,915 | 5,794 | 1,418 | 24,127 | 3 |
| Directive (tactical), closes a gap | closing-decision | 18,989 | 3,229 | 1,418 | 23,636 | 3 |
| Directive (tactical, settled), 1 ref, closes a fact | closing-decision | 31,226 | 5,053 | 1,418 | 37,697 | 4 |
| Done, 1 ref, closes a plan | closing-done | 32,055 | 3,478 | 1,418 | 36,951 | 4 |
| Done, closes a directive | closing-done | 21,642 | 1,763 | 1,418 | 24,823 | 3 |
| Done, closes a gap | short-loop | 21,642 | 3,126 | 1,418 | 26,186 | 3 |
| Fact, closes a question | dissolution | 18,989 | 1,732 | 1,418 | 22,139 | 2 |
| Gap, closes a fact | closing-signal | 18,989 | 1,551 | 1,418 | 21,958 | 2 |
| Gap, supersedes a gap | supersedes | 16,915 | 1,555 | 1,418 | 19,888 | 2 |
| Aspiration | aspiration-capture | 16,915 | 12,452 | 1,418 | 30,785 | 3 |
| Actor | actor-capture | 16,915 | 3,659 | 1,418 | 21,992 | 2 |
| Role, 1 ref | role-capture | 27,328 | 1,882 | 1,418 | 30,628 | 3 |
| Annotation, 1 ref | annotation-capture | 1,720 | 1,914 | 1,418 | 5,052 | 1 |
| Focus, 1 ref | focus-capture | 1,720 | 2,585 | 1,418 | 5,723 | 1 |

Sections of the system prompt that a guard adds (`internal/llmops/preflight_templates/universal_system.tmpl:5-10`, guards in `internal/llmops/preflight.go:305-334`):

| Section | Bytes | Guard |
|---|---:|---|
| Unrelated references check | 1,193 | draft has refs |
| Reference metadata consistency, rules and calibration | 9,220 | draft has refs |
| Unusual close-pattern check | 2,074 | draft has closes |
| Artifact durability | 2,653 | kind `done` |
| Settled-directive justification | 1,824 | directive with `intent: settled` |

Not in the rules text, although pre-flight's own prompt carries them: the validator role, the answer format and the severity scale, and the kind-fit rule (`kind_fit.tmpl`), which `PreflightRubric` never renders because it sets no kind fit.

### Contracts

Eight active contracts, quoted as entry blocks (`internal/llmops/preflight.go:503-510`, rendered by `FormatEntryForPrompt`, `internal/llmops/summary.go:410`). Blocks 14,017, separators 49, heading 68, together 14,134. The checks annotation-capture and focus-capture carry no contracts.

| Contract | Block | Body |
|---|---:|---:|
| `d-cpt-vt1` | 719 | 530 |
| `d-cpt-ah1` | 1,244 | 1,089 |
| `d-cpt-e1i` | 814 | 680 |
| `d-prc-8vh` | 1,157 | 960 |
| `d-prc-zb6` | 1,078 | 953 |
| `d-cpt-i2x` | 1,073 | 658 |
| `d-cpt-chi` | 1,521 | 1,070 |
| `d-cpt-7iy` | 6,411 | 5,873 |

The last one is 45% of the contracts block.

### Graph-fit axes

1,418 bytes, the `light_axes` template in rubric mode (`internal/llmops/preflight_templates/light_reading.tmpl`, rendered at `internal/llmops/preflight.go:172`). The four axes `duplicates`, `outdated`, `strain` and `dismissal` take 1,011 bytes with the heading and lead, their severity bands 407.

## 2. The thirteen checks, rules against calibration

Thirteen checks (`internal/llmops/preflight.go:205-217`), selected by `selectCheckType` (`:339`). Sizes are the check's part of the rendered rules text. Calibration is its `## Calibration` section with any sub-calibration.

| Check | Variant | Rules | Calibration | Template file |
|---|---|---:|---:|---:|
| signal-capture | any signal without closes | 2,215 | 1,051 | 2,107 |
| decision-refs | plan | 5,101 | 1,105 | 7,299 |
| decision-refs | directive at tac, ops or prc | 4,794 | 1,000 | |
| decision-refs | directive at stg or cpt | 5,163 | 2,361 | |
| decision-refs | settled directive, tactical | 4,946 | 1,115 | |
| decision-refs | settled directive, conceptual | 5,315 | 2,476 | |
| closing-decision | closes a gap | 2,105 | 1,124 | 4,137 |
| closing-decision | settled directive closes a fact | 2,506 | 2,547 | |
| closing-done | closes a plan | 1,134 | 2,344 | 3,767 |
| closing-done | closes another decision | 787 | 976 | |
| short-loop | done closes a gap | 1,193 | 1,933 | 3,352 |
| dissolution | fact closes a question | 706 | 1,026 | 1,961 |
| closing-signal | gap closes a fact | 753 | 798 | 1,776 |
| aspiration-capture | | 800 | 1,022 | 2,141 |
| supersedes | | 735 | 820 | 1,788 |
| actor-capture | | 2,560 | 1,099 | 3,845 |
| role-capture | | 1,136 | 746 | 2,183 |
| annotation-capture | own system block | 1,569 | 345 | 2,430 |
| focus-capture | own system block | 2,165 | 420 | 3,257 |

- decision-refs: 1,866 of its rule bytes are the section "Augmenting directives", served to every decision without closes, plans included (`decision_refs.tmpl:8-20`).
- signal-capture, decision-refs and closing-decision each embed the layer check with the 460-byte layer guide.
- aspiration-capture also quotes the six active aspirations, 10,630 bytes (`aspiration_capture.tmpl:23-27`).

## 3. What code already decides

Pre-flight's mechanical checks run in every capture. `preflightEntry` calls `mechanicalFindings` (`pkg/application/capture.go:65-70`, `:104`), which calls `Finder.MechanicalPreflight` (`internal/finders/preflight.go:72`) and `mechanicalPreflight` (`internal/finders/preflight_mechanical.go:65`).

| Category | Function in `preflight_mechanical.go` |
|---|---|
| `participant-drift` | `participantCoverageFindings` (`:335`) |
| `focus-actor-drift` | `focusActorCoverageFindings` (`:372`) |
| `ref-kind-missing`, `ref-kind-invalid` | `refKindFindings` (`:125`) |
| `ref-kind-inapplicable` | `refKindApplicabilityFindings` (`:154`) |
| `supersede-non-head` | `supersedeForkFindings` (`:301`) |
| `cross-repo-dep-undeclared`, `cross-repo-ref-unresolved` | `crossRepoResolutionFindings` (`:236`) |
| `actor-canonical-reused`, `actor-kind-changed` | `actorWriteOnceFindings` (`:415`), `actorKindStabilityFindings` (`:444`) |
| `role-canonical-mismatch`, `role-refs-missing-head` | `roleMechanicalFindings` (`:479`) |
| `procedure-canonical-reused`, `serve-budget` | `procedureWriteOnceFindings` (`:523`), `serveBudgetFindings` (`:98`) |

None of them is in the rules text as a rule. The rules text holds prose that points at them:

- Ref-kind applicability: the paragraph "Applicability is not your question" (`ref_meta_consistency.tmpl:8`, 443 bytes with its blank line). It names the `Admissible ref kinds for this target` lines of the referenced entries section, which the rules text drops. The verifier gets those lines under "Ref fit" (`deepRefFit`, `pkg/application/workflow_deep.go:614`; `DeepRefFit`, `internal/llmops/preflight.go:605`), and each of them ends in "Applicability is settled mechanically. Do not flag it." (`ref_applicability.tmpl:12`).
- Attachment links: "a separate mechanical check owns those" (`attachment_claim.tmpl:12`).
- "Mechanical frontmatter checks run separately" and "Trust mechanical layer": `actor_capture.tmpl:4`, `role_capture.tmpl:4,21`, `annotation_capture.tmpl:27`, `focus_capture.tmpl:36`.
- "Presence-of-field checks ... belong to CLI/lint": `decision_refs.tmpl:41`.

Rules that code decides elsewhere and whose prose is still in the rules text:

- **Acceptance criteria of a plan.** The draft validation refuses a plan without the section `## Acceptance criteria` and one `- [ ]` item (`internal/model/construction.go:478-482`). The rule and its `high` band are in the rules text of a plan (`decision_refs.tmpl:28` and `:36`), 307 and 105 bytes, together 412.
- **Guards.** In rubric mode Go evaluates each guard and drops a rule whose guard fails (`internal/llmops/preflight.go:305-334`). A rule that stays keeps its guard prose, for example `durability.tmpl:2,4`, `settled_justification.tmpl:2,4`, `ref_meta_consistency.tmpl:2`, `unusual_close.tmpl:22`.
- **Close patterns.** The rule "Unusual close-pattern check" (2,074 bytes) is in the rules text of every draft with closes. It lists six standard patterns (`unusual_close.tmpl:6-11`) and asks for no finding on them (`:22`). No code tells a standard pattern from an unusual one. `validateCloses` (`internal/model/graph.go:1156`) refuses three closes and allows every other.
- **Language.** With no language configured the template takes its no-op branch. Those 101 bytes are in every rules text.
- **Check selection.** `selectCheckType` picks the check from the draft's shape. Each check's `## Task` restates that shape.

## 4. The ref-kind vocabulary by part

The vocabulary is the body of the ref-kind fact, demoted (`refKindVocabulary`, `internal/llmops/preflight.go:676`). 5,719 bytes:

| Part | Bytes |
|---|---:|
| Heading, opening paragraph, paragraph on direction | 1,431 |
| Table head | 73 |
| Table rows, nine kinds | 2,400 |
| Five paragraphs on neighbouring kinds | 1,453 |
| Mechanics | 351 |
| Blank lines between the parts | 11 |

Table rows: `grounded-in` 348, `builds-on` 236, `refines` 267, `addresses` 399, `surfaces` 214, `surfaced-by` 325, `depends-on` 164, `required-by` 226, `related` 221.

Kinds `model.AdmissibleRefKinds` (`internal/model/ref_applicability.go:171`) admits per target class, by the matrix `refKindMatrix` in the same file:

| Target class | Admitted | Not admitted |
|---|---|---|
| live decision | all nine | none |
| live signal | all nine | none |
| terminal done | seven | `refines`, `addresses` |
| retired | eight | `refines` |

Rows a vocabulary narrowed to the admitted kinds loses:

| Ref targets of the draft | Rows lost | Bytes |
|---|---|---:|
| at least one live decision or live signal | none | 0 |
| done entries only | `refines`, `addresses` | 666 |
| retired entries only, or retired and done entries | `refines` | 267 |

## 5. Estimate after the three removals

Removed: the acceptance-criteria rule of a plan (412), the applicability paragraph (443), the close-pattern rule where every close is a standard pattern (2,074). The vocabulary counts as unchanged, as for a draft with one live ref target. The figures are the measured totals minus those sizes. No rules text was rendered without the pieces.

| Shape | Today | After | Less |
|---|---:|---:|---:|
| Gap signal, 1 ref | 32,012 | 31,569 | 443 |
| Plan, 1 ref | 34,952 | 34,097 | 855 |
| Directive (tactical), 1 ref | 34,540 | 34,097 | 443 |
| Plan, 1 ref, closes a gap | 34,049 | 31,532 | 2,517 |
| Directive (conceptual), 1 ref | 36,270 | 35,827 | 443 |
| Directive (tactical, settled), 1 ref | 36,631 | 36,188 | 443 |
| Directive (conceptual, settled), 1 ref | 38,361 | 37,918 | 443 |
| Directive (tactical, settled), 1 ref, closes a fact | 37,697 | 35,180 | 2,517 |
| Done, 1 ref, closes a plan | 36,951 | 34,434 | 2,517 |
| Done, closes a gap | 26,186 | 24,112 | 2,074 |

## 6. The cheap passes before the deep verifier

In a project with `verify.mode: deep`:

- **The light prose verifier does not run.** `preflightEntry` returns after the mechanical checks and the rules text (`pkg/application/capture.go:65-74`), before `lightPreflight` (`:76`) and `finder.Preflight` (`:80`). `deepJudgesPreflight` (`pkg/application/workflow_deep.go:176`) holds while the project runs deep and no skip of the deep exchange covers the draft. After a skip, pre-flight's model runs.
- **The writing guide runs, before the deep verifier.** `guideDue` is "the writing guide has not judged this capture yet" and has no term for the mode (`pkg/application/workflow_timing.go:196-197`). It runs on the project's LLM runner (`pkg/application/writingguide_api.go:86`), the one pre-flight uses (`pkg/application/capture.go:62`).
- **The guide and the rules text share no axis.** The guide judges `stranding`, `dilution`, `conflation`, `pointing` and `form` (`internal/llmops/writingguide_templates/writing_guide.tmpl:29-37`). The axes of the rules text are `duplicates`, `outdated`, `strain` and `dismissal`.
- Those four are also axes of the deep verifier itself (`deepAxes`, `internal/model/verifier_deep.go:42`), so the 1,418-byte section restates axes the verifier holds.

## 7. The finding schema

A deep finding carries no identifier of the rule it applies. `deepFindingSchema` (`internal/engine/schema.go:555-594`) allows `verdict`, `axis`, `severity`, `owner`, `pointers`, `basis`, `sources` and `edits`, and no other property. `model.DeepFinding` has the same fields (`internal/model/verifier_deep.go:175-184`).

`axis` is a closed set of ten: contradicts, duplicates, outdated, inconsistent, ref-fit, missing-ref, dismissal, aspiration, strain, evidence. No value names a pre-flight check or rule. Pre-flight's own findings carry a free `category` (`internal/llmops/response_schemas/preflight.json`). A deep finding raised from the rules text takes one of the ten axes, and its rule shows only in the words of `verdict`. The capture lists a finding as `f<n> [severity, axis]: verdict` (`pkg/application/workflow_deep.go:790`).

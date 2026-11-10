---
type: signal
layer: process
kind: fact
override: closed
confidence: high
topics:
    - engine/base-facts
    - type-system/refs
index:
    title: 'Choosing a reference kind: each kind''s meaning and direction, tests between neighboring kinds'
    topic: type-system/refs
summary: >-
    Reference kinds connect entries: a ref is a contextual pointer with no
    status effect, written on the source, naming why it points at its target.
    Covers each kind's direction, when it applies over a sharper or weaker
    neighbor, and the calibration that keeps related a floor, not a default,
    and a terminal done never "addressed".
---

# Connecting entries: the reference kinds

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

{{ .Mechanics }}

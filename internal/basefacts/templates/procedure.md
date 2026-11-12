---
type: signal
layer: process
kind: fact
override: closed
confidence: high
topics:
    - engine/base-facts
    - type-system/kinds
refs:
    - id: 20260814-100000-s-prc-psr
      kind: related
      desc: the spec reference — the frontmatter fields, variable types, step shapes, and the live ability registry an author consults when actually writing the workflow
summary: >-
    Procedure kind commits a repeatable way of working as a runnable entry.
    Every other entry is read; a procedure is also run. Canonical name
    everything binds to; typed step workflow in frontmatter the engine runs
    with guaranteed structure; per-step guidance in the body. Revised by
    supersession under the same canonical. Workflow's structural validation
    runs at engine load, not capture.
---

# Extending the process with workflows: the procedure kind

Procedure records a committed way of working that runs: a repeatable dialogue the engine executes step by step. Capturing an entry, a roastery's weekly cupping review, cutting a release. Every other entry is read; a procedure is also run. Directive, activity, plan record something (direction held, work committed, outcome to reach); graph holds them as records. Procedure instead defines a workflow the agent operates step by step while the engine holds the structure. Step cannot be skipped. Gate holds until what it checks is true. Question declared for the user stops everything until the user answers. That is what the kind buys: dialogue with guaranteed structure, not guidance an agent might drift from. Use it only when defining something to run.

**The canonical is the identity.** Procedure is named by its `canonical`: short, stable name (`capture`, `release`, `cupping`) everything binds to. Sessions start the move by canonical; other procedures dispatch it by canonical; entry ID stays out. Canonical is written once, never reused for another procedure. Revising means superseding the current head under the same canonical. Closed chain still owns its name. Pick the word the team already uses for the routine.

**Class places how it enters.** Every procedure carries one execution role:

{{ .Classes }}

**The workflow is frontmatter; the guidance is body.**

- `params`: what a start may pass in
- `state`: working fields steps collect and read
- `steps`: the walk itself; each step collects, serves, advances

What a step wires in (checks it gates on, data it serves, actions it performs) are framework abilities, named from the engine's live registry. Step that asks the user covers whatever no ability does. Body carries one instruction unit per step (`## unit: <step>`). Unit is the authoritative guidance served while that step runs. Write it to the person or agent in the situation, never as documentation about the procedure. Pointer inside a unit says why and when to read a fact; it never tries to replace it.

**Ask where the choice is real.** Gate advances on what must hold. Agent chooser records the agent's judgment with evidence. User chooser stops the run until the user answers. Gate what is mechanical. Keep user choosers for what is truly the user's to decide: a confirmation that writes, a direction only they can set. Asking the user to confirm what a gate could check is ceremony. Gating what only the user can judge is worse.

**Shipped and project procedures.** Some procedures ship with the framework, always present. Project's own procedures live in its graph beside them. They may freely depend on the project's environment: tools, paths, services it actually uses. Overriding a shipped procedure is ordinary supersession of its chain head. Newer shipped version and project override compete for the head? Project's wins for execution. The fork is flagged for deliberate, merge-style reconciliation, never automatic.

**Retire deliberately.** Supersede to revise: same canonical, new head. Practice ended without replacement? Retire with a directive closing the procedure head, stating why the move is no longer how work happens. Never edit: the chain is the practice's history.

**Validation waits for the engine.** Capture checks entry shape: canonical, class, layer. Workflow soundness (step wiring, guards, abilities it names) is judged when the engine loads the spec. Broken workflow fails loudly there, not silently at capture.

{{ .Mechanics }}

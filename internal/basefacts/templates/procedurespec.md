---
type: signal
layer: process
kind: fact
override: closed
confidence: high
topics:
    - engine/base-facts
    - engine/base-procedures
refs:
    - id: 20260813-170000-s-prc-prd
      kind: related
      desc: the procedure kind's authoring fact — what a procedure is and when to reach for the kind; this reference covers how to write its spec
summary: >-
    Procedure spec reference: frontmatter fields a runnable procedure
    declares (typed params and state, step list with gates, choosers and
    transitions). Closed sets of variable types and gateable field names. How
    a run gets initial values and hands work to another procedure. Worked
    example valid by construction. Live ability registry that guards, injects
    and ops draw from.
---

# Writing a procedure spec

Procedure entry's frontmatter declares the workflow the engine executes. Three fields carry it, beside the entry's usual ones: `params`, `state`, `steps`. Engine runs the workflow over a per-run variable store; params and state declare its fields.

## `params`: what a caller passes at start

Each entry is a declaration, `name: {type, optional?, desc}`. Params are written into the store once, at start.

## `state`: the working fields of the run

Same declaration shape as params. Steps collect and read these fields as the run advances. Field arriving from a dispatching parent, not the caller, is state, not a param: a parent's seed writes state only.

**Field names are a vocabulary, not a free choice.** Commonest gate is a presence check: passes once one specific field is present and non-empty. Each check binds to its field by name: `hasAnchor` reads `anchor`; nothing reads a name outside this vocabulary. So a field is gateable on presence only with one of these names:

{{ .GateableFields }}

Prefer these names over invented near-synonyms (`brief`, not `timeline`). Field outside the vocabulary can still be collected and served, but no gate can hold on it. Declaring stays your job. Guard naming a check whose field the spec never declares loads cleanly, then never advances: run waits on a field nothing can collect. This list, like the type list below, renders from the running engine's own declarations each time the fact is served. It is the running version's truth and cannot disagree with the live registry.

Declaration attributes:

- `type`: a domain type, or `list<T>` around one; closed set below.
- `optional`: `true` marks the field not required where collected.
- `desc`: one line, served in the step's report schema: the field's shape and its constraint, such as a field it pairs with or what an empty value means. How to fill the field belongs in the step's unit, which the agent reads beside the schema.
- `default`: state only. Applied at start when no channel supplied the field. The one literal a spec carries, in the type's natural YAML form (`true`, a quoted string, a list).

**How a run's store fills.** Initial values arrive through three channels. Name them apart: "seed" is casually used for all three. Rank order:

1. Value passed explicitly at start wins. Params always arrive this way; a start may also carry any *declared state field* the caller sets.
2. Dispatching parent's armed seed writes next (state only; see Dispatching below).
3. `default` applies last, only where nothing else supplied the field.

Everything after start is collected by steps.

## `steps`: the walk itself

List in execution order. Each step has an `id` and advances one of three ways.

### Gate steps

Gate declares `transitions`: ordered `{when: <guard>, to: <step id>}` clauses, optionally closed by `{otherwise: <step id>}`. Guard is a boolean combination of ability names written infix: `when: hasBrief and hasSynthesis`. Only `and`, `or`, `not` and parentheses. No comparisons, no literals, no field access. Gate may also run one effect first (`op: <command name>`).

### Chooser steps

Agent chooser (`chooser: agent`) or user chooser (`chooser: user`) declares `options`: `{choice, collect?, call?, dispatch?, to}`. Fields the option collects, optional command it calls, optional seed declaration for a handoff (below), and where it goes. User chooser stops the run until the user answers. Option may route back to an earlier step, which then serves again. Confirm-or-correct loop needs no machinery beyond a user chooser whose options route on the answer.

### On any step

- `collect`: state fields reportable here. `?` suffix marks one optional (quote the marker: `"anchor?"`). Required ones hold the step until reported.
- `inject`: list of `{fn: <query name>, args?, maxBytes?, maxItems?}` calls. Each result is served into the step's instructions under its function's name. Size is the engine's job: inject without a declared cap is bounded by engine defaults; `maxBytes`/`maxItems` override.
- `render`: name of an extra `## unit:` section from the body, served with the step (typically presenting one collected field's content).
- `framing`: session framing blocks a serve of this step carries: `info` (participant, language, search, branch), `health`, or the id of a lane the session shell declares, such as `principles` or `recentMovement`. Left out, the step carries every block. Declare `[info]` on a step that only waits, launches or confirms, where the shell's lanes add nothing. A name the session does not frame fails the serve.

Run ends by transitioning to {{ .EndTargets }}.

### Serve budget

Every automatic serve is bounded per part. Individually capped parts can still sum past a host's response budget. Capture pre-flight and the graph's lint sweep size each step at its declared worst case. Step over the engine's default serve budget raises an advisory finding. Finding is a risk note, never a load failure: spec still runs. To accept the trade, declare `serveBudget: <bytes>` at the spec's top level (beside `params`/`state`/`steps`). Declared total at or above the worst case silences the finding and records the decision on the spec itself.

## Variable types

Declaration's `type` is a domain type, or `list<T>` around one. Declare the semantic type only. Concrete shape a value must take is generated from the declaration: object fields, identifier patterns, accepted values of closed enumerations such as the ref-kind set or the confidence grades. It is served as the step's report schema while the procedure runs, then enforced when the value arrives. Nothing about shape needs restating in the spec. Closed set:

{{ .VarTypes }}

## Abilities

Guards name checks; `inject` names queries; `op` and `call` name commands. Engine serves the live inventory of all three through its function registry, with each ability's contract (what it reads, what it writes). Where the engine is connected, its `registry` tool returns that inventory by class (predicate, query, command), including each query's argument names. What it lists is exactly what a spec can name, for the running version. Beyond the presence checks above, this reference deliberately carries no ability list: pull the registry when you write steps. Step asking the user is always available for whatever no ability covers.

## Dispatching another procedure

A step never starts another procedure; no command does. Handoff lives in the unit's prose. Instruction tells the running agent to start the named procedure as a sub-move of this run. Product comes back the same way: agent reports it into a field this spec declares, and the next gate holds on that field. Child that declares no delivery returns nothing else: it writes nothing into this run's store. The reported field is the only bridge.

Option's `dispatch` block declares seeding for that handoff, never the start itself. `dispatch: {procedure: <canonical>, seed: {<child field>: <parent field>}}` on the answered option arms the mapping. Next procedure started under this run receives each value into the *state* field it declares under the child name. Value passed explicitly at start wins. Empty source seeds nothing.

Delegate procedure (class `task`) is dispatched the same way, inputs resolved by the dispatching run. It asks the user nothing and is offered nowhere on its own. So the dispatching unit's prose is where it is discoverable.

### Delegates that deliver

Task whose gate declares `deliver: [<field>, …]` hands its product back through the engine, not the agent's prose. It runs in its own delegate session: no user, its own reads, the dispatching run's branch and project. Forked by the answer to an option whose `dispatch` names it. That call forks the delegate; its serve carries the dispatch handoff, which names the launch. Never started by hand. When one of that gate's transitions fires, the named fields are delivered into the dispatching run. Transition to `end(completed)` is the final delivery and completes the delegate. Any other target is an interim delivery that leaves it running. So one delegate can exchange several rounds with its dispatching run. Task with an interim delivery declares `rounds: <n>` at its top level. After that many interim deliveries, it concludes at its next waiting gate instead of waiting.

Waiting is a gate that declares `await: [<field>, …]`. It holds until each field holds a delivery from the other side not yet taken; passing takes it. On the delegate's side the dispatching run fills it; on the dispatching side the delegate does. Only a delivery fills an awaited field: no step collects one, no report writes it. Delivery never moves the receiving run's step; receiver picks it up on its next call. Gate declaring `awaitAny: [<field>, <field>, …]` instead holds only until one of them holds such a delivery. Passing takes them all; its transitions tell which one came by a predicate reading the fields that arrived. Dispatching gate declaring `delegateEnded: <step>` goes to that step once none of the run's delegates is live and none is about to be forked: nothing is left to deliver what it waits for (a delegate that concluded, failed or was torn down before delivering). Without it the gate keeps waiting.

Answered option's `dispatch` maps the names, so the task never depends on its dispatcher's:

- `receive: {<parent field>: <child field>}` lands each delivered child field in a parent field (also delivery-only).
- `send: {<child field>: <parent field>}` delivers the parent field into the child field the delegate awaits, every time the dispatching run writes it: by a report, an answer or a command of its own.

Every field the task delivers must be received; every field it awaits must be sent. Dispatch missing one is refused when the answer forks the task. Delegate ends with its dispatching run: when that run completes or is abandoned, its delegates are abandoned too.

## Instruction units

Entry's body carries one `## unit: <step id>` section per step. Served as the instructions the agent works from while that step runs. Write it as what to do there and how to report the step's fields. Step's `render` field may name an extra unit section beyond the per-step ones. Units may use template placeholders over the store's fields and the step's injected data: Go-template form, each field by declared name. `The anchor under review: {{"{{.anchor}}"}}` serves with the store's `anchor` value in place. Inject's result appears under its function's name the same way. Field not yet collected renders empty.

## A worked example

Small review move, written for width, not realism: optional param and optional collect, two injects with differently shaped arguments, compound guards, user chooser looping back for correction, dispatch seed on the confirming option, closing gate on the field the dispatched recording hands back. Ships from the same source the engine's own tests load, so it is valid by construction:

```yaml
# one frontmatter: the entry's identity fields, then the workflow it runs
type: decision
kind: procedure
layer: process
canonical: example-review
class: move
{{ .Example }}```

Workflow sits in the same frontmatter as the entry's usual fields (references, topics, confidence). Body carries one `## unit:` section per step: here `## unit: scope`, `## unit: account`, `## unit: review`, `## unit: record`. Each is served verbatim as the instructions the agent works from while that step runs: what to do there, how to report the step's fields. Review unit, for example, presents the account for the user's ruling. It instructs that a confirming answer leads to starting the recording procedure as a sub-move.

## Validation

Spec structure (step wiring, guard grammar, every named ability exists) is validated when the engine loads the entry. Broken spec fails loudly there. Procedure that loads becomes startable by its canonical and appears in the served inventory of moves. Start small, load it, run it, grow it by superseding the entry. Workflow's identity is its supersession chain; canonical keeps resolving to the chain's newest version.

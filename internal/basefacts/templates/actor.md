---
type: signal
layer: process
kind: fact
override: closed
confidence: high
topics:
    - engine/base-facts
    - type-system/kinds
summary: >-
    Actor kind records who a participant is: a declared identity, human or
    not. Canonical name matches what the graph already uses, never reused for
    another identity, introduced readably in prose; aliases collected in the
    same capture. Pinned to the process layer, evolves by supersession within
    its chain, retired with the why by a closure that cascades to the
    identity's roles.
---

# Declaring a participant: the actor kind

Actor records who a participant is: a person, a team, a model family. Any identity the graph attributes work to. A signal: identity declared, not committed to. Declaration is the point: participants are declared, never inferred. An entry may name a participant before any actor declares them. Actor captured later takes that name as its canonical. Until then the name attributes work without saying who is behind it. Reader asks: who is this, what name does the graph know them by, which other names resolve to them? Good actor answers all three, shortest.

**The canonical is the name already in use.** Canonical is the stable name this participant is known by across the graph. The form they would recognize as theirs, matched to the name work is already attributed under. Not a more formal variant invented for the record. Village choir's record already says "Marta"? Canonical is "Marta", not the full name from her papers. Before drafting, resolve the identities already active and their aliases. Known participant is reused, not reconfirmed.

**The canonical outlives the entry.** Everything binds to the name, not to the entry that introduced it. Canonical is the handle attributions are written against; it stays stable across every later supersession. Canonical that named one identity never names another: attributions written years ago must still resolve to the right participant. So even a departed participant's name is not free for reuse. Routine participation needs no pointer back. Listing someone among an entry's participants already carries the identity reference; the entry does not also point at the actor for taking part.

**Introduce the canonical in the prose.** One sentence is enough: "Yusuf is a master baker trained in Gaziantep, twenty years at his family's bakery before this one". But it must be there. Capture naming the canonical only in fields is an identity record with no readable identity. Bare naming with nothing behind it ("actor: Yusuf") is its own defect. Prose grounds who this is for a future reader with no outside context: affiliation, background, expertise brought from elsewhere. Prose also holds mutable context: job title, organization, current focus. These change too often for a fresh entry; they sit in prose because they change without the identity changing.

**Identity here, not work here.** Body describes who the participant is, independent of work in this project. What they do inside it is a role: separate entry bound to the actor. Village bakery: "Marta: trained mezzo, twelve years in the city opera chorus, teaches music at the Gymnasium" is an actor. "Marta selects the repertoire for the winter concert" is her role. People naturally describe a participant in mixed terms. Normal: it means two entries, not a blend. Draft actor first: role binds to its canonical and never mints a name of its own. Capturing both is the default. Deferring the role is an explicit call, never a silent drop of what the person volunteered.

**Aliases travel with the capture.** In the same capture, collect other names this participant appears under: name they sign work with, go by in conversation, use on outside correspondence. List each once. Alias is for the reader, never a second canonical. Work is attributed by canonical alone. Aliases exist so a signature or chat name resolves back to the identity. So when aliases are present, prose says where each variant appears. Village bakery: "signs invoices as J. Weber, appears in the group chat as Jojo". Resolution is then readable, not guessed.

**Say whether the participant is human.** `actor_kind` records it as a field, `human` or `machine`, because a tool cannot read prose. Assistant may propose work, but a person answers for what gets committed. Consumer deciding who may own something needs an answer it can trust. Optional; absent means unknown, never human. Identity that is neither (a team, say) carries no value until one exists. Fixed across the chain: supersede renames a participant or revises prose, never reclassifies one.

**Declare the identity that persists.** Humans and non-humans are actors alike: a person and the model family assisting the project both participate. For anything non-individual, declare the stable identity over the momentary instance. Model family, not one working session. Team, not tonight's roster. Identity scoped to a moment forces a new actor every time the moment changes. Attribution then scatters across handles that were never really different participants.

**Who participates is how the project works.** Actor describes the project's working fabric, not any piece of the work. Pinned to the process layer, whatever depth the participant's own work runs at. Person doing strategic thinking is not a strategic-layer entry.

**Evolving is superseding; leaving is retirement.** Identity itself shifts (canonical changes, alias added or removed, spelling corrected)? Supersede the actor within its own chain. Same canonical across a supersede is normal. First capture naturally points at nothing: identity arrives from the world, declared. Supersede points at what it replaces. Retirement differs. Participant no longer relevant (left the project, will not participate further)? Closing entry retires the actor with the why, most naturally a directive. Done is a category error: nothing completed. Closing the identity closes its work-side shadow with it. Every role ever bound to any of the identity's names retires in the same stroke; no separate role closures to write.

{{ .Mechanics }}

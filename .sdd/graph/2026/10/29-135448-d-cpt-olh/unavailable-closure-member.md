# Unavailable dependency-closure member: design record

Dialogue, Christopher and Claude, 2026-10-28 to 2026-10-29, prompted by the second Greptile review round on pull request #23 (shared closure walk, typed repo IDs, closure as every read's horizon). Code references are to branch `worktree-closure-walk-repoid` at `f97765f6`.

## Prompt

Greptile round 2 raised two findings. Both were fixed in pull request #23 and stay its scope:

1. `start_session` with a connected, cached repository outside the served project's closure opened a session there. Fixed in `1867f7e8`: the local composition resolves a project beyond the home only inside the served project's closure.
2. The application's closure walk read every member's source view to learn its declarations, so a search naming a healthy dependency failed when a sibling's snapshot could not be read. Fixed in `943330e6` and `f97765f6`: the shared closure step treats a member whose source view cannot be read as declaring nothing; release and cancellation errors still fail. Session gate and read horizon follow the one rule.

Reviewing the fix exposed that serve, on `main` already, loads the home's direct dependencies up front for `show` and for a session's graph and fails both when a direct dependency's graph cannot be read (`requireSources` in `snapshotWithDependencyPolicy`, introduced with acquired reads, 20260906-234056-d-tac-bae), while a capture tolerates the same failure. That is the open question 20260903-153224-s-cpt-ir1, so the dialogue settled the failure model as a whole.

## Behaviour on 2026-10-28, as observed from outside

| Situation | CLI | serve |
|---|---|---|
| member not connected or not cloned, read does not name it | warning on stderr (a connected, uncloned member is cloned first) | works, no warning |
| a direct member's graph cannot be read, read does not name it | not checked | `show` and session open fail |
| a member's configuration is malformed | read fails naming the repository | resolver error treated as unavailable, cause dropped |
| search or view naming an unavailable member | search skips it with a warning; view marks its section unavailable | fails with "dependency unavailable" |
| all repos with one member unavailable | reads the rest, warns | fails |
| `show <repo-id>:<entry-id>` with the repo unavailable | not checked | "entry not found" |
| reference into an unavailable member in a rendered chain | `[unresolved: repo X]` | `[unresolved: repo X]` (shared presenter) |
| capture referencing an entry in an unavailable member | blocks, after fetch on miss | blocks, no fetch (20261027-145612-s-tac-2n5) |

## Decided rules (numbered as agreed)

1. A read never fails because of a member it does not need: a sibling met while walking the closure, a direct dependency loaded up front for `show` or a session's graph, a member behind another member.
2. A read naming one member fails when that member is unavailable, naming the member and the reason: working in it as a project, `show <repo-id>:<entry-id>`, search or view with that repository selected. Both surfaces; the CLI's skip-with-warning for a named repository goes. When the member is readable but its refresh failed, the read proceeds on the cache and warns.
3. A read over all repos returns what it can read and names every member it could not read.
4. A reference into an unavailable member renders as unavailable and the read continues: `show` upstream and downstream chains keep the ref line and its description, with a marker in place of the target's summary and status; view expansions and lists likewise. A capture keeps blocking per reference (20260628-130400-d-cpt-uh0), only on references into the unavailable member, naming the reason.
5. One warning per unavailable member, the same kinds on every surface: not connected on this machine, not cloned, refresh failed, configuration unreadable, graph unreadable, and plain unavailable for a hosted refusal. Each carries who must act and the original error text (20260829-113416-d-cpt-nut). The CLI prints it to stderr; serve carries it in the read's result beside the incomplete-index notice and in session framing beside the graph-health block.
6. A hosted refusal stays indistinguishable from an unknown dependency (20260712-104530-d-tac-1mb). A failure does not hide behind it: `AccessResolver.ResolveDependency` separates a refusal (a typed not-available answer) from an error (reported verbatim). Public API change for implementers, including `pkg/sddtest` and `examples/extendingsdd`.
7. A member reachable only through an unavailable member is unreachable too; a read naming it names the chain and the unavailable member rather than "not in the closure".
8. Refreshing caches from serve stays with 20261027-145612-s-tac-2n5; surfacing a member's unreadable entries stays with 20260723-114403-s-tac-m13.
9. Recorded as a pending directive; implemented as a follow-up to pull request #23, which carries only the two review fixes.

Structural requirement, from Christopher on rules 4 and 7: unavailability is a typed Go value (member, kind, original error, chain) carried from detection to the presenters; only the presenters render text, through one rendering shared by CLI and serve.

## Example renderings (illustrative, not normative)

```
- related example.test/b:20260713-040000-s-tac-dep [unavailable: repo example.test/b, not cloned]
  ↳ the ref's own description stays

warning: example.test/b is not cloned on this machine (user: run `sdd repo sync`): <original error>
warning: example.test/c is not reachable: example.test/a → example.test/b (graph unreadable: <original error>)
```

## Alternatives weighed

- Require access (today's serve behaviour, the strict answer of 20260903-153224-s-cpt-ir1): rejected. A broken direct dependency stops every session in the home project, and in engine mode, where no cache is ever refreshed, one uncloned second-level member fails every all-repos search and view.
- A named repository in search or view as additive to the local graph, returning local results plus a warning: rejected for an explicitly named repository, because an agent can miss a warning and report the repository as holding nothing.
- Keep the gate strict and only the read horizon tolerant (the first shape of the Greptile fix in `943330e6`): rejected; two walks over one closure with different error rules make a member readable that a session may not work in.
- Name only "not in the closure" for a member hidden behind an unavailable one: rejected; the cause stays invisible.

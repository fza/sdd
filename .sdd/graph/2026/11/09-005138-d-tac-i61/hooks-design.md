# Capture hooks: configured extension points at every capture step

Configured commands that run at the steps of the capture procedure, for checks that report findings into a step (a prose linter) and for status that must reach an external orchestrator at every step. Separate from the verifier design.

## 0. Owner decisions

| # | Topic | Decision | Why | Rejected | Accepted cost |
|---|---|---|---|---|---|
| H-D1 | What a hook can do | One kind. A hook names an event (a capture step with `entered` or `left`, for example `playback.entered`, `preflight.left`) and a command. sdd runs the command synchronously and writes the step's JSON (event, session, instance, step, draft) to its stdin. Exit 0 passes; an optional JSON output `{"findings": [...]}` in pre-flight's finding format joins that step's findings, a `high` one blocking as in pre-flight. A nonzero exit or a timeout fails the call loud, so a status hook never misses a step unnoticed | One contract for observers and checkers; findings reuse the pre-flight format and blocking rule; fail-loud delivery is what an orchestrator needs to rely on | Two kinds `observe` and `check` (two contracts, a hook doing both declared twice); status hooks first, checks later (the linter use waits) | A slow hook slows every step it is attached to |
| H-D2 | Who may declare hooks | Hooks are read only from the user-global and local config layers (`~/.config/sdd/config.yaml`, `.sdd/config.local.yaml`). A `hooks` key in the committed `.sdd/config.yaml` is refused when the config resolves, naming the file and the layers that accept it | A hook runs code on the machine running sdd; only the person on that machine chooses what runs there, the same rule as the verifier's custom driver (V-D19) | Committed hooks after a one-time approval per machine (`sdd hooks allow` with a hash of the block, as direnv does; an approval surface to build and keep, and code still arrives through a commit); committed hooks without approval (anyone who can commit config runs code everywhere) | A project cannot ship a hook; each contributor configures it, and setups can drift |
| H-D3 | Events | Engine-level, for every shipped procedure: `<procedure>.<step>.<entered|left>`, with `*` matching any step or procedure, plus `entry.written` after a publish. The capture's deep verification step (verifier design V-D21: after pre-flight, before write) is an event like every other step, `capture.verify.<entered|left>`, with one more event per round, `capture.verify.round`. Event names are checked against the running procedures when the config resolves; an unknown name is refused, listing the valid ones. Findings are accepted only on steps with a findings channel (`assemble`, `guide`, `preflight`, `verify`); a hook returning findings elsewhere is refused at config time | One hook point in the engine's transition loop serves capture and every other procedure, so status covers session starts, catch-ups and evaluations too; the verification step is the longest-running part of a capture, so an orchestrator needs its start, rounds and end | Capture only (status for other procedures needs a second mechanism, the engine still needs the same hook point) | Step ids become a public surface; renaming a step breaks hooks naming it, loudly at config time |
| H-D4 | Several hooks on one event, timeouts | As Claude Code runs its hooks: every hook matching an event runs in parallel and to completion before sdd merges the results; one hook's failure or blocking finding does not stop its siblings. Findings from all hooks merge into the step, and any `high` among them blocks. Per-hook `timeout` in seconds, default 600. Unlike Claude Code, which discards a timed-out hook's output and takes no decision from it, a failure or timeout of any hook fails the call once all have finished (H-D1) | A known model for hook authors; parallel runs keep a step's wait at the slowest hook instead of the sum | Sequential runs in config order (wait is the sum of all hooks, a failure would hide later hooks' results) | Two hooks on one event deliver in no fixed order; a hook cannot rely on another's side effect |

## 2. Payload (design detail following H-D1 to H-D4)

One JSON object on stdin, the same over MCP and CLI since hooks fire in the engine:

```json
{"event": "capture.verify.round", "time": "<RFC 3339>",
 "project": "<project>", "branch": "<branch>",
 "session": "<session id>", "procedure": "capture", "instance": "<instance id>",
 "step": "verify", "phase": "entered | left | round",
 "transition": "<next step or end state, on left only>",
 "draft": {"type": "...", "kind": "...", "layer": "...", "refs": [], "topics": [],
           "body": "...", "attachments": ["<name>"], "body_hash": "<sha256>"},
 "findings": [],
 "verify": {"mode": "light | deep", "launch": "...", "round": 2, "open": 1}}
```

1. `draft` only in capture; `findings` only on steps with a findings channel, holding the findings so far; `verify` only on the verification step.
2. Hook stdout: nothing, or `{"findings": [...]}` in pre-flight's finding format (H-D1).
3. Unknown fields are ignored by readers; new fields may be added without a version bump.

## 1. Ground truth

1. Go-level extension points exist only for compositions embedding sdd: `MutationFinalizer`, a named idempotent effect after each write (`pkg/application/graphstore.go:136`), and the engine's `EventSink` receiving every session event (`internal/engine/session.go:93`). Neither is configurable.
2. No config-level hook exists.
3. Running a command with JSON on stdin needs only the standard library (`os/exec`); no dependency.

# Serve token cost: measurements and design record

## Measurements

### Repeat share of MCP results

Claude Code transcripts, sdd MCP tool results deduplicated by tool-use id, the served-once memory switched off by `sessions.token_saving: off`.

| Sample | Results | Bytes | Exact repeats of a part the same agent context already received |
|---|---|---|---|
| One large capture session in a client project | 71 `next`, 6 `start_procedure`, 9 `gather` and others | 1.58 MB | 1.05 MB (66%) |
| 116 transcripts, 2026-11-08 onward | 941 | 12.99 MB | 50% |
| All transcripts, 2026-11-10 to 2026-11-11 | 7,038 | 105 MB | 50.9 MB (48%) |

Repeat split in the single session: report schema 42%, framing blocks 38% (`principles` alone 203 KB, about 2.7 KB on every serve), instruction lanes 13%, the session opening lane 4%, the sync notice 2.5%. About 80% of the repeats are static text.

The served-once memory was off because an external hook was meant to collapse repeats. That hook covered only `show` and stopped running after 2026-11-09. From then on nothing collapsed repeats, and the average result grew from about 4 KB to about 15 KB per call.

### Deep verifier launches

- One fresh launch reads 180 to 360 KB: 45 to 85 KB of sdd results plus 107 to 272 KB of file reads on reading pages.
- 61 of about 244 verifier forks ended as "a new deep exchange replaced it". 46 of those 61 came after an accepted proposal followed by a one-line change.
- 65.2% of all verifier page bytes re-read pages an earlier verifier of the same parent agent had already received.
- `DeepPageChars = 50000` caps the page text, not the serialized result. Claude Code saves any MCP text result over 50,000 characters to a file. 36 results were saved that way and read back through 27 file reads totalling 1.43 MB; 9 saved pages were never read. A one-line JSON file is returned whole on every read.
- The pre-flight rules page repeats the draft and its referenced entries (about 7 to 10 KB) and carries sections whose own heading says they do not apply (2 to 6 KB).
- The capture's `deepWait` step serves a 14 to 15 KB report schema for a 135-byte instruction; every revise schema carries `procedureSpec` (3.3 KB) whatever the draft's kind.

### JSON against plain text

Opus input tokens counted through `claude -p` for the largest serve of each tool, base prompt 19,118 tokens:

| Serve | JSON | Text | Change |
|---|---|---|---|
| `next` | 4,789 | 4,623 | -3.5% |
| `start_procedure` | 13,341 | 12,800 | -4.1% |
| `search` | 3,924 | 3,989 | +1.7% |
| `show` | 18,469 | 18,787 | +1.7% |
| `view` | 4,283 | 4,327 | +1.0% |
| total | 44,806 | 44,526 | -0.6% |

Serves are mostly prose that JSON carries compactly; key names are 4 to 12% of the bytes, mostly in the report schema.

### Proof of holding after compaction

Compaction summaries keep identifiers: four Claude Code summaries in one session kept the session handle and several entry ids. A plain tag would survive a summary.

An earlier spike (grounding design, D34) measured a check word at a random position: 68 of 70 correct verdicts, 6 to 16 times the output tokens of a plain pointer, counting distracted from the verdicts. Neither harness lost the served text there.

Anchor-proof spike, haiku, 5 runs per arm, candidates served as tool output, unrelated work, then a lossy compaction:

| Host and arm | Correct verdicts | Proofs passed without a re-read | Re-reads | Output tokens |
|---|---|---|---|---|
| Claude Code, text held | 50/50 | 50/50 correct proofs | 0 | 2,012 |
| Claude Code, after compaction | 50/50 | 0 | 50 | 3,567 |
| Claude Code, pointer without proof | 50/50 | n/a | 50 | 3,003 |
| Chat (simulated), text held | 49/50 | 50/50 correct proofs | 0 | 2,174 |
| Chat (simulated), after compaction | 50/50 | 0 | 50 | 2,870 |
| Chat (simulated), pointer without proof | 49/50 | n/a | 7 | 2,593 |

No summary carried a proof. Without the proof the simulated Chat agent judged 43 of 50 entries from its summary. Codex was not run. Threats: one model, small samples, Chat simulated, words after a tag often function words, one run wrote `REREAD` in the proof field after re-reading.

## Host facts

- Claude Code: `PreToolUse` can rewrite MCP input; `SessionStart` sources `startup|resume|clear|compact|fork`; no hook fires for a subagent's own compaction; no session identifier reaches MCP servers; results over 50,000 characters are saved to a file.
- Codex 0.167: project hooks in `.codex/hooks.json`, each trusted by hash before it runs; `PreCompact` carries the subagent id; `tools/call` `_meta` carries `threadId` and `sessionId`; MCP elicitation forms since 0.64.
- A remote connector carries MCP only. Plugins can carry hooks, which Chat ignores and Cowork and Claude Code load. Chat compacts long conversations with no signal to MCP servers.

## Alternatives weighed

| Alternative | Why not chosen |
|---|---|
| Keep `sessions.token_saving: off` | About half of every result repeats; the hook it existed for no longer runs |
| Turn the switch back on unchanged | The memory is per sdd session and blind to compaction: after one it points at text the agent lost |
| Hooks tagging every call with agent id and a reset counter | Needs installed hook files, git checks, Codex trust, one elicitation per session; Chat cannot run hooks at all; the anchor proof covers every host |
| Hooks where available, tags elsewhere | Two mechanisms to keep in step, once the anchor proof passed |
| Plain possession tags | Summaries keep identifiers, so a tag can outlive its text |
| Check word at a counted position | 6 to 16 times the output tokens, attention drawn from the task |
| Agent-echoed lineage token | One retained token vouches for every piece at once |
| sdd estimating the agent's context | Cannot rule out a pointer to lost text; usable only as a backstop |
| Plain-text results | -0.6% input tokens; clients, tests and eval harnesses rely on the structure |
| Collapsing repeated gather brief lines | Relevance judging needs the brief at hand (D34) |
| Fresh verifier with a smaller reading | Less context to judge a change against the whole draft |
| Skipping re-verification for small wording edits | A one-word change can change the meaning |
| Running prose hooks before the verifier | The observed refusal came from a project's own agent hook on draft files, outside sdd |

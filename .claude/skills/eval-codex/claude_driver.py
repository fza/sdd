#!/usr/bin/env python3
"""Drive a real Claude Code agent through headless `claude -p` against a scratch
SDD repository, one user turn per invocation.

  claude_driver.py setup REPO [--task TEXT]      scratch repo: git, sdd init for Claude, anchor directive, code
  claude_driver.py start REPO MESSAGE [opts]     new Claude session, first user turn
  claude_driver.py say REPO MESSAGE              next user turn, resuming the same session
  claude_driver.py log REPO [--turn N] [--max N] what the sdd tools served, in full, subagents included
  claude_driver.py transcripts REPO              the session's transcript files: main, then subagents
  claude_driver.py status REPO                   Git state: branches, worktrees, WIP markers

State lives beside the repo in REPO.eval/, never inside it: session id, each
turn's stream, and a Claude config directory of its own (CLAUDE_CONFIG_DIR), so
transcripts, the project list and memory stay out of the user's ~/.claude. The
user's login is shared, never copied: CLAUDE_SECURESTORAGE_CONFIG_DIR points the
credential lookup at the user's own config directory.

Each turn is one `claude -p` process; background work the agent starts ends with
the turn that began it.
"""
import argparse
import json
import os
import subprocess
import sys
import threading
import time
from pathlib import Path

sys.dont_write_bytecode = True  # no __pycache__ in the skill directory
sys.path.insert(0, str(Path(__file__).resolve().parent))
import driver  # noqa: E402

DEFAULT_SDD = driver.DEFAULT_SDD
SDD_PREFIX = "mcp__sdd__"
ALLOWED_TOOLS = ["mcp__sdd", "Bash", "Skill"]
# AskUserQuestion needs a UI inside the turn; without it the agent asks in its final message, as in a chat.
DISALLOWED_TOOLS = ["AskUserQuestion"]


def state_dir(repo):
    return driver.state_dir(repo)


def load_state(repo):
    path = state_dir(repo) / "state.json"
    if not path.exists():
        sys.exit(f"no session for {repo}; run `start` first")
    return json.loads(path.read_text())


def save_state(repo, state):
    (state_dir(repo) / "state.json").write_text(json.dumps(state, indent=2))


def config_dir(repo):
    path = state_dir(repo) / "claude-config"
    path.mkdir(exist_ok=True)
    return path


def trust(repo):
    """Accept the repo's trust dialog in the run's config, as its user would have; -p skips the dialog and
    otherwise ignores the repo's permission settings."""
    path = config_dir(repo) / ".claude.json"
    config = json.loads(path.read_text()) if path.exists() else {}
    config.setdefault("projects", {}).setdefault(str(repo), {})["hasTrustDialogAccepted"] = True
    path.write_text(json.dumps(config, indent=2))


def run_env(repo):
    """The caller's environment without its Claude session's variables, pointed at the run's config directory."""
    env = {k: v for k, v in os.environ.items() if not k.startswith("CLAUDE")}
    env["CLAUDE_CONFIG_DIR"] = str(config_dir(repo))
    # Empty selects the default credential item; a user with their own CLAUDE_CONFIG_DIR keeps theirs.
    env["CLAUDE_SECURESTORAGE_CONFIG_DIR"] = os.environ.get(
        "CLAUDE_SECURESTORAGE_CONFIG_DIR", os.environ.get("CLAUDE_CONFIG_DIR", ""))
    env["DISABLE_AUTOUPDATER"] = "1"
    return env


def mcp_config(repo, sdd):
    path = state_dir(repo) / "mcp.json"
    path.write_text(json.dumps({"mcpServers": {"sdd": {"type": "stdio", "command": str(sdd), "args": ["serve"],
                                                       "alwaysLoad": True}}},
                               indent=2))
    return path


def claude_args(repo, state):
    args = ["claude", "-p", "--output-format", "stream-json", "--verbose",
            "--mcp-config", str(mcp_config(repo, Path(state["sdd"]))), "--strict-mcp-config",
            "--setting-sources", "project",
            "--permission-mode", state["permission_mode"], "--permission-prompts", "none",
            "--allowedTools", *ALLOWED_TOOLS, "--disallowedTools", *DISALLOWED_TOOLS,
            # Sibling worktrees live beside the repo.
            "--add-dir", str(repo.parent)]
    if state.get("model"):
        args += ["--model", state["model"]]
    if state.get("effort"):
        args += ["--effort", state["effort"]]
    if state.get("session"):
        args += ["--resume", state["session"]]
    return args


def transcript_paths(repo, session):
    """The session's main transcript and its subagents' transcripts."""
    projects = config_dir(repo) / "projects"
    main = sorted(projects.glob(f"*/{session}.jsonl"))
    subagents = sorted(projects.glob(f"*/{session}/subagents/*.jsonl"))
    return main, subagents


def served(content):
    """An sdd tool result as a dict, when it returned JSON."""
    if isinstance(content, list):
        content = "".join(p.get("text", "") for p in content if isinstance(p, dict) and p.get("type") == "text")
    try:
        value = json.loads(content)
    except (ValueError, TypeError):
        return None
    return value if isinstance(value, dict) else None


def result_text(content):
    if isinstance(content, list):
        return "".join(p.get("text", "") for p in content if isinstance(p, dict) and p.get("type") == "text")
    return content or ""


def calls(events):
    """Tool calls in stream order: (call, result block or None), each call carrying its parent_tool_use_id."""
    order, results = [], {}
    for event in events:
        if event.get("type") not in ("assistant", "user"):
            continue
        content = (event.get("message") or {}).get("content")
        if not isinstance(content, list):
            continue
        for block in content:
            if block.get("type") == "tool_use":
                order.append({**block, "parent": event.get("parent_tool_use_id")})
            elif block.get("type") == "tool_result":
                results[block.get("tool_use_id")] = block
    return [(call, results.get(call["id"])) for call in order]


def describe(call, result):
    name, args = call["name"], call.get("input") or {}
    error = result is not None and result.get("is_error")
    if name.startswith(SDD_PREFIX):
        serve = served(result.get("content")) if result else None
        serve = serve or {}
        position = " ".join(f"{k}={serve[k]}" for k in ("procedure", "step", "status") if serve.get(k))
        missing = f" missing={serve['missing']}" if serve.get("missing") else ""
        failed = f" ERROR {driver.brief(result_text(result.get('content')))}" if error else ""
        return f"[sdd] {name[len(SDD_PREFIX):]} {driver.brief(args)} -> {position}{missing}{failed}"
    if result is None:
        status = " (no result)"
    else:
        status = " (error)" if error else ""
    if name == "Bash":
        return f"[sh] {driver.brief(args.get('command', ''), 200)}{status}"
    if name in ("Edit", "Write", "MultiEdit", "NotebookEdit"):
        return f"[edit] {args.get('file_path') or args.get('notebook_path')}{status}"
    if name in ("Agent", "Task"):
        return f"[agent] {args.get('subagent_type', 'general-purpose')}: {args.get('description', '')}{status}"
    if name == "Skill":
        return f"[skill] {args.get('skill')} {args.get('args', '')}{status}"
    return f"[{name}] {driver.brief(args)}{status}"


def print_turn(number, events, elapsed):
    result = next((e for e in reversed(events) if e.get("type") == "result"), {})
    print(f"== turn {number}: {result.get('subtype', 'no result')} in {elapsed:.0f}s"
          f" (cost ${result.get('total_cost_usd', 0):.4f})")
    init = next((e for e in events if e.get("type") == "system" and e.get("subtype") == "init"), None)
    if init and number == 1:
        servers = ", ".join(f"{s['name']}:{s.get('status')}" for s in init.get("mcp_servers") or [])
        print(f"== live MCP servers: {servers or 'none'}")
        print(f"== agents: {', '.join(init.get('agents') or [])}")
        print(f"== skills: {', '.join(init.get('skills') or [])}")
    for call, res in calls(events):
        indent = "    (sub) " if call["parent"] else "  "
        print(indent + describe(call, res))
    for denial in result.get("permission_denials") or []:
        print(f"  denied: {denial.get('tool_name')} {driver.brief(denial.get('tool_input'))}")
    print("== agent says:")
    print(result.get("result") or "(no message)")


def run_turn(repo, state, text, timeout):
    state["turns"] = state.get("turns", 0) + 1
    number = state["turns"]
    stream = state_dir(repo) / "turns" / f"{number:03d}.jsonl"
    stream.parent.mkdir(exist_ok=True)
    stderr = state_dir(repo) / "claude.stderr.log"
    trust(repo)
    events, started = [], time.time()
    with open(stream, "w") as out, open(stderr, "a") as err:
        proc = subprocess.Popen(claude_args(repo, state), cwd=repo, env=run_env(repo), stdin=subprocess.PIPE,
                                stdout=subprocess.PIPE, stderr=err, text=True, bufsize=1, start_new_session=True)
        proc.stdin.write(text)
        proc.stdin.close()
        timer = threading.Timer(timeout, proc.kill)
        timer.start()
        try:
            for line in proc.stdout:
                out.write(line)
                if not line.strip():
                    continue
                try:
                    event = json.loads(line)
                except ValueError:
                    continue
                events.append(event)
                if event.get("type") == "system" and event.get("subtype") == "init" and not state.get("session"):
                    state["session"] = event["session_id"]
                    save_state(repo, state)
            proc.wait()
        finally:
            timer.cancel()
    save_state(repo, state)
    with open(state_dir(repo) / "turns.jsonl", "a") as log:
        log.write(json.dumps({"turn": number, "user": text, "stream": str(stream)}) + "\n")
    print_turn(number, events, time.time() - started)
    main, subagents = transcript_paths(repo, state.get("session", ""))
    for path in main + subagents:
        print(f"== transcript: {path}")
    if not any(e.get("type") == "result" for e in events):
        reason = f"killed after {timeout}s" if time.time() - started >= timeout else f"exited with {proc.returncode}"
        sys.exit(f"claude {reason} without a result; see {stderr} and {stream}")


def cmd_setup(args):
    flags = ["--agents", "claude", "--verify-mode", args.verify_mode]
    if args.verify_mode == "deep":
        flags += ["--verify-runner", args.verify_runner]
    if args.dialogue_mode:
        flags += ["--dialogue-mode", args.dialogue_mode]
    driver.setup_repo(args, flags)


def cmd_start(args):
    repo = Path(args.repo).resolve()
    if (state_dir(repo) / "state.json").exists():
        sys.exit(f"a session exists for {repo}; `say` continues it, or remove {state_dir(repo) / 'state.json'}")
    state = {"sdd": str(Path(args.sdd).resolve()), "model": args.model, "effort": args.effort,
             "permission_mode": args.permission_mode, "session": None, "turns": 0}
    save_state(repo, state)
    run_turn(repo, state, args.message, args.timeout)


def cmd_say(args):
    repo = Path(args.repo).resolve()
    state = load_state(repo)
    if not state.get("session"):
        sys.exit(f"the first turn on {repo} never reported a session id; `start` a new run")
    run_turn(repo, state, args.message, args.timeout)


def cmd_log(args):
    repo = Path(args.repo).resolve()
    for line in (state_dir(repo) / "turns.jsonl").read_text().splitlines():
        record = json.loads(line)
        if args.turn and record["turn"] != args.turn:
            continue
        print(f"==== turn {record['turn']} — user: {record['user']}")
        events = [json.loads(l) for l in Path(record["stream"]).read_text().splitlines() if l.strip()]
        for call, res in calls(events):
            if not call["name"].startswith(SDD_PREFIX):
                continue
            who = " (sub)" if call["parent"] else ""
            print(f"----{who} [sdd] {call['name'][len(SDD_PREFIX):]} {json.dumps(call.get('input'), ensure_ascii=False)}")
            content = res.get("content") if res else None
            serve = served(content)
            text = json.dumps(serve, indent=1, ensure_ascii=False) if serve else result_text(content) or "(no result)"
            print(text if len(text) <= args.max else text[:args.max] + f"\n… ({len(text)} chars)")


def cmd_transcripts(args):
    repo = Path(args.repo).resolve()
    main, subagents = transcript_paths(repo, load_state(repo).get("session", ""))
    for path in main + subagents:
        print(path)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest="command", required=True,
                                     metavar="{setup,start,say,log,transcripts,status}")

    setup = commands.add_parser("setup")
    setup.add_argument("repo")
    setup.add_argument("--sdd", default=str(DEFAULT_SDD))
    setup.add_argument("--summary", default="Add a farewell function to the greeting module.")
    setup.add_argument("--task", default=(
        "Add a `farewell(name)` function to `greeting.py` that returns `Goodbye, <name>!`, "
        "with a unit test in `test_greeting.py`."))
    setup.add_argument("--verify-mode", default="deep", choices=["off", "grounding", "prose", "deep"])
    setup.add_argument("--verify-runner", default="subagent", choices=["subagent", "claude", "codex", "driver"])
    setup.add_argument("--dialogue-mode", choices=["guided", "auto"])
    setup.set_defaults(func=cmd_setup)

    start = commands.add_parser("start")
    start.add_argument("repo")
    start.add_argument("message")
    start.add_argument("--sdd", default=str(DEFAULT_SDD))
    start.add_argument("--model", default="sonnet", help="sonnet, opus, or a full model name")
    start.add_argument("--effort", choices=["low", "medium", "high", "xhigh", "max"])
    start.add_argument("--permission-mode", default="acceptEdits",
                       choices=["acceptEdits", "dontAsk", "bypassPermissions"],
                       help="acceptEdits: edits inside the repo and its parent, Bash, sdd tools and skills run; "
                            "anything else is denied")
    start.add_argument("--timeout", type=int, default=1800)
    start.set_defaults(func=cmd_start)

    say = commands.add_parser("say")
    say.add_argument("repo")
    say.add_argument("message")
    say.add_argument("--timeout", type=int, default=1800)
    say.set_defaults(func=cmd_say)

    log = commands.add_parser("log")
    log.add_argument("repo")
    log.add_argument("--turn", type=int)
    log.add_argument("--max", type=int, default=6000)
    log.set_defaults(func=cmd_log)

    transcripts = commands.add_parser("transcripts")
    transcripts.add_argument("repo")
    transcripts.set_defaults(func=cmd_transcripts)

    status = commands.add_parser("status")
    status.add_argument("repo")
    status.set_defaults(func=driver.cmd_status)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()

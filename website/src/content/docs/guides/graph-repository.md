---
title: A graph in its own repository
description: Keep the graph in a repository of its own, beside the repository where the work happens.
---

The graph can live in a repository of its own. The code repository's history then carries only the work, and every `sdd: …` commit lands in the graph repository. Name the graph repository when you set the code repository up:

```bash
cd project && sdd init --graph-repo git@github.com:acme/project-sdd.git
```

On a terminal, a fresh `sdd init` without the flag asks where the graph lives: in this repository, or in a repository of its own on a remote.

```text
project/                       # only work commits
  .sdd/config.yaml             # graph_repo, skill_scope, supported_agents, hooks
  .sdd/config.local.yaml       # participant and other personal settings for this machine
  .claude/skills/              # sdd init installs and commits these here
  .mcp.json                    # sdd serve
  src/

github.com/acme/project-sdd    # only sdd: commits
  .sdd/config.yaml             # graph_dir, repo_id, default_branch, language, dependencies, ...
  .sdd/meta.json
  graph/2026/09/...
```

sdd reads and writes the graph repository through a clone of its own under `~/.local/share/sdd/repos/`; `sdd repo path <repo_id>` prints where. The first command that needs the graph makes the clone. Nobody works in it: sdd discards hand edits there. `sdd init --graph-repo` sets up `.sdd/` in a graph repository that holds none, with the graph under `graph/`, and pushes it.

`graph_repo` holds the graph repository's `repo_id`, such as `github.com/acme/project-sdd`. The code repository's committed config carries `graph_repo`, `skill_scope`, `supported_agents` and [hooks](/sdd/reference/configuration/#hooks), nothing else: every shared project setting, the graph and `meta.json` live in the graph repository, so a committed config that sets any other key beside `graph_repo` is refused, naming the file and the key. `graph_dir` is the one exception: a config file that sets `graph_repo` may set it too, and it is ignored there; the graph repository's own config names the graph folder. A graph repository whose own config sets `graph_repo` is refused.

Personal settings stay in the code repository: `.sdd/config.local.yaml` there is the project's machine-local file, and `sdd config set --local` writes it.

Every command run in the code repository acts on the graph repository: captures commit in the clone and are pushed, `sdd serve` serves it, and the write gate reads its `meta.json`. The work itself stays where it is: `sdd init` installs skills and registers the MCP server in the code repository, `sdd wip start` branches it, and closure evidence comes from its history.

## A graph folder outside the repository

`graph_dir` may also lead out of the repository holding `.sdd/`, as a relative or an absolute path, into another repository or into none. sdd reads the entries there and writes new ones there. The settings of the repository holding `.sdd/` apply; a `.sdd/` in the repository the graph folder lies in is not read.

sdd commits a capture with git in the working directory. git refuses a file outside that repository, so the capture fails after its entry file is written, and the file stays uncommitted. To keep a graph in another repository with its commits, use `graph_repo`.

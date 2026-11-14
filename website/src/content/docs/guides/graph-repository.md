---
title: A graph in its own repository
description: Keep the graph in a repository of its own, beside the repository where the work happens.
---

The graph can live in an sdd project of its own, beside the repository where the work happens. The work repository's history then carries only the work, and every `sdd: …` commit lands in the project's repository. Set the project up first, then point the work repository at it:

```bash
cd project-sdd && sdd init
cd ../project && sdd init --project-dir ../project-sdd
```

```text
project/                     # only work commits
  .sdd/config.yaml           # project_dir, skill_scope, supported_agents
  .sdd/config.local.yaml     # optional: project_dir for this machine
  .claude/skills/            # sdd init installs and commits these here
  .mcp.json                  # sdd serve
  src/

project-sdd/                 # only sdd: commits
  .sdd/config.yaml           # graph_dir, repo_id, default_branch, language, dependencies, ...
  .sdd/config.local.yaml     # participant and other personal settings for this project
  .sdd/meta.json
  .sdd/graph/2026/09/...
```

`project_dir` names the folder holding the project's `.sdd/`, relative to the work repository's root or absolute. The work repository's files carry nothing else: every project setting, the graph, `meta.json`, sessions and stats live in the project, so a work repository that sets any other key is refused, naming the file and key. A `graph_dir` leading into another repository is refused the same way, since commits, the schema and engine branches all belong to the repository holding the project's `.sdd/`.

Every command run in the work repository acts on the project: captures commit there, `sdd serve` serves it and works on its branches (a session binds a branch checked out in the project's repository), and the write gate reads its `meta.json`. The work itself stays where it is: `sdd init` installs skills and registers the MCP server in the work repository, `sdd wip start` branches it, and closure evidence comes from its history.

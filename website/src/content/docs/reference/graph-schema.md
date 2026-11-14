---
title: Graph schema
description: What the graph schema version means and how sdd summarize --all fills missing titles.
---

`.sdd/meta.json` records the graph's schema version. A binary whose schema is newer than the graph's keeps writing at the graph's schema; a graph newer than the binary is refused until sdd is upgraded.

The graph schema is 1. An entry's `title` is optional at schema 1: older binaries ignore it, and a listing shows the summary's first sentence for an entry without one. `sdd summarize --all` fills the missing titles. It first commits titles that an earlier run wrote but failed to commit, then states what the backfill costs in model calls and runs. An entry that already has a summary keeps it, and the title comes from a title-only call. On a terminal a progress bar counts the filled entries; otherwise each entry gets its own line. An interrupted backfill continues on the next `sdd summarize --all`.

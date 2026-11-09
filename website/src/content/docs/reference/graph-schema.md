---
title: Upgrading the graph schema
description: How sdd migrate upgrades a graph to the schema of a newer sdd binary.
---

A binary whose graph schema is newer than the one `.sdd/meta.json` records keeps writing at the graph's schema, and `sdd migrate` upgrades the graph. It states what the new schema adds and what the backfill costs in model calls, then runs only on your approval (`y`, or `--yes`). It stamps the new version first, which locks older binaries out, then fills what the graph lacks. An interrupted backfill continues on the next `sdd migrate`.

The graph schema is 1. An entry's `title` is optional at schema 1: older binaries ignore it, and a listing shows the summary's first sentence for an entry without one. `sdd migrate` on a schema-1 graph fills the missing titles, after the same approval; an entry that already has a summary keeps it, and the title comes from a title-only call.

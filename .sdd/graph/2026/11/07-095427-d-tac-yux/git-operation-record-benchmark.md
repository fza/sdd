# Rewriting the git operation record as JSON: measurement

Question: is rewriting one JSON file whole, on every git network operation sdd starts, fast enough, or does the record need a format that updates one slice in place (an index plus fixed or versioned data slices)?

## Method

A Go program (standard library only) models the record: six host and protocol buckets with 300 timestamps each (the worst case for an hourly window at 300 operations an hour), plus one record per repository (protocol, authenticated flag, last attempt, last success, outcome, retries, next fetch, next push). One cycle reads the file, decodes it, changes one repository record and one bucket, encodes it, writes a temporary file, optionally calls fsync, and renames it over the original. 50 cycles per size, on an Apple silicon Mac with an internal SSD, Go 1.26.

## Results

| Repositories | File size | p50 | p95 | p50 with fsync | p95 with fsync |
|---|---|---|---|---|---|
| 100 | 94 KB | 1.1 ms | 2.5 ms | 5.1 ms | 6.3 ms |
| 1,000 | 367 KB | 3.6 ms | 4.1 ms | 8.1 ms | 9.3 ms |
| 5,000 | 1.6 MB | 15.2 ms | 18.3 ms | 20.9 ms | 24.3 ms |
| 20,000 | 6.3 MB | 63.0 ms | 86.0 ms | 60.3 ms | 74.0 ms |

## Reading

- sdd writes the record at most a few times a minute. At 1,000 repositories one write holds the cross-process lock for under 10 ms with fsync.
- Most of the 94 KB base size is the timestamp lists. Per-minute counters (60 per bucket) instead of timestamps shrink the file and make the window cheaper to evaluate.
- An in-place slice format would need its own binary layout, version handling, compaction and a checksum per slice against torn writes. Writing a temporary file and renaming it gives crash safety without any of that.
- The format is worth revisiting only for a machine holding tens of thousands of repositories.

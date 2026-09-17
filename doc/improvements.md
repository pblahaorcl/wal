# Improvements

## High priority

- Add log segmentation and retention/compaction so the WAL does not grow
  without bound and startup does not need to scan one ever-growing file.
- Add an append-batch API that writes several records and performs one sync at
  the durability boundary.
- Define and enforce the multi-process behavior. Either add an advisory file
  lock or document clearly that a path may only be opened by one process.
- Add crash-recovery tests that exercise torn headers, torn payloads, checksum
  failures, failed syncs, and recovery after a partially written append.
- Add a verification or repair command/API for checking a log without opening
  it for appends and for reporting the first invalid record with useful
  diagnostics.

## API and performance

- Add methods for common log-management operations such as `Len`,
  `LastSequence`, replay from a sequence, and reading a sequence range.
- Reduce lock contention for reads and replay. Metadata can be snapshotted
  under the mutex while independent `ReadAt` calls happen outside it, with
  close and file-lifetime behavior defined explicitly.
- Reuse buffers or expose an opt-in lower-allocation read path for workloads
  that process many records.
- Consider configurable checksum and record-format versions so the on-disk
  format can evolve without making old logs unreadable.
- Add optional preallocation and configurable sync policies for workloads that
  need predictable append latency.

## Correctness and operations

- Add tests for empty records, maximum-size records, invalid options, sequence
  exhaustion, concurrent appends/reads, and the race detector.
- Make recovery behavior explicit for a truncated final record versus a
  complete record with invalid data, and expose enough error detail for
  operators to distinguish them.
- Consider syncing the parent directory when creating or replacing a log if
  crash-durable file creation is required.
- Document portability and durability guarantees across operating systems and
  filesystems, including what `Sync` and `Close` guarantee.
- Add metrics/hooks for append latency, bytes written, replay progress, and
  recovery events.

## Documentation and tooling

- Document the binary record layout and compatibility policy.
- Add examples for batched writes, replaying from a checkpoint, corruption
  handling, and graceful shutdown.
- Add a small inspection tool that prints record counts, byte usage, sequence
  ranges, and corruption details.
- Expand benchmarks to cover batch appends, different payload sizes, replay
  sizes, recovery, and concurrent readers.

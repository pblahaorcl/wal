# WAL data file structure

This document describes the current on-disk format written by the `wal`
package. A log is one append-only file containing zero or more records. There
is no file-level header, index, footer, padding, or alignment: records are
stored back-to-back from byte offset zero.

## Record layout

Each record has a fixed 20-byte header followed by its payload:

```text
  0                   4                   12          16          20
  +-------------------+-------------------+-----------+-----------+----------
  | magic (4 bytes)   | sequence (8)      | length (4) | CRC-32 (4) | payload
  +-------------------+-------------------+-----------+-----------+----------
```

All integer fields use unsigned big-endian encoding. The payload is an
arbitrary byte string and may be empty.

| Relative offset | Size | Field | Encoding and meaning |
| --- | ---: | --- | --- |
| `0` | 4 | Magic | ASCII `WAL1` |
| `4` | 8 | Sequence | `uint64`, big-endian; starts at `1` and increases by one for every record |
| `12` | 4 | Payload length | `uint32`, big-endian; number of payload bytes, not including the header |
| `16` | 4 | Checksum | `uint32`, big-endian; CRC-32/IEEE of the payload only |
| `20` | `N` | Payload | The `N` bytes described by the length field |

The total encoded size of a record is `20 + N` bytes. The next record starts
immediately after the payload. For example, a record with a five-byte payload
occupies 25 bytes, so a following record starts at offset `0x19`, even though
that is not a 16-byte hexdump line boundary.

The file therefore has this structure:

```text
record 1: header(sequence=1, length=N1, checksum=CRC1) + payload 1
record 2: header(sequence=2, length=N2, checksum=CRC2) + payload 2
record 3: header(sequence=3, length=N3, checksum=CRC3) + payload 3
...
```

The default maximum payload size is 64 MiB. `WithMaxRecordSize` can lower or
raise that limit up to the format's `uint32` length-field limit. The limit is
checked both when opening an existing file and when appending a new record.

## Single-record hexdump

Appending `[]byte("create account")` to a new log produces one record. The
payload is 14 bytes, sequence is `1`, and the CRC-32/IEEE checksum is
`0x2a7e429f`:

```text
$ xxd -g1 -c16 events.wal
00000000: 57 41 4c 31 00 00 00 00 00 00 00 01 00 00 00 0e  WAL1............
00000010: 2a 7e 42 9f 63 72 65 61 74 65 20 61 63 63 6f 75  *~B.create accou
00000020: 6e 74                                            nt
```

The record decodes as follows (the first four fields are the 20-byte header):

```text
57 41 4c 31                         magic = "WAL1"
00 00 00 00 00 00 00 01             sequence = 1
00 00 00 0e                         payload length = 14
2a 7e 42 9f                         CRC-32/IEEE("create account")
63 72 65 61 74 65 20 61 63 63 6f 75  payload = "create accou..."
```

The payload continues with `6e 74` (`nt`) at offset `0x20`.

## Two-record hexdump

If a new log receives `hello` followed by `world`, the file contains two
25-byte records. The first record starts at `0x00`; the second starts at
`0x19`:

```text
$ xxd -g1 -c16 events.wal
00000000: 57 41 4c 31 00 00 00 00 00 00 00 01 00 00 00 05  WAL1............
00000010: 36 10 a6 86 68 65 6c 6c 6f 57 41 4c 31 00 00 00  6...helloWAL1...
00000020: 00 00 00 00 02 00 00 00 05 3a 77 11 43 77 6f 72  .........:w.Cwor
00000030: 6c 64                                            ld
```

The bytes can be split at the record boundary as follows:

```text
offset 0x00
  57 41 4c 31                         magic = "WAL1"
  00 00 00 00 00 00 00 01             sequence = 1
  00 00 00 05                         payload length = 5
  36 10 a6 86                         CRC-32/IEEE("hello")
  68 65 6c 6c 6f                      payload = "hello"

offset 0x19
  57 41 4c 31                         magic = "WAL1"
  00 00 00 00 00 00 00 02             sequence = 2
  00 00 00 05                         payload length = 5
  3a 77 11 43                         CRC-32/IEEE("world")
  77 6f 72 6c 64                      payload = "world"
```

## Opening and recovery

`Open` scans the file from the beginning and validates each complete record.
For every record it checks the magic, the next expected sequence number, the
declared length, and the payload checksum.

- An empty file is a valid empty log.
- If the final record is incomplete—either fewer than 20 header bytes remain,
  or the declared record extends past end-of-file—it is treated as a torn
  write. The file is truncated back to that record's starting offset and
  synced before `Open` returns.
- A complete record with an invalid magic, non-contiguous sequence, excessive
  length, or checksum mismatch is corruption. `Open` returns an error wrapping
  `wal.ErrCorrupt` and does not silently repair that record.

For example, the following six bytes after two valid records are an incomplete
tail:

```text
... last complete record ... 57 41 4c 31 00 00
                              \__________/
                              partial next header
```

On the next `Open`, those bytes are removed and the next append reuses the
sequence number that the partial record would have received.

## Durability and compatibility

`Append` and `AppendBatch` write complete encoded records with `WriteAt`. With
sync-on-write enabled (the default), the file is synced before the append
returns. A batch writes all of its records and then performs one sync. When
`WithSyncOnWrite(false)` is used, callers must call `Sync` (or `Close`) at the
desired durability boundary.

`WAL1` identifies the current format. The reader requires sequence numbers to
be contiguous starting at one, so records must not be reordered, removed from
the middle, or manually edited. There is currently no separate format-version
negotiation or forward-compatibility mechanism; an incompatible future layout
needs a new magic value or an explicit versioned format.

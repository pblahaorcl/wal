# wal

`wal` is a small append-only write-ahead log for Go. Records are checksummed,
numbered from one, and persisted with `fsync` by default.

```go
log, err := wal.Open("events.wal")
if err != nil {
    return err
}
defer log.Close()

sequence, err := log.Append([]byte("order-created: 1234"))
if err != nil {
    return err
}

entry, err := log.Read(sequence)
if err != nil {
    return err
}
fmt.Println(string(entry.Data))
```

For several records, `AppendBatch` returns their sequence numbers and performs
one sync at the durability boundary:

```go
sequences, err := log.AppendBatch([][]byte{
    []byte("item-added: 1"),
    []byte("item-added: 2"),
})
```

Call `Replay` after opening to rebuild application state:

```go
err := log.Replay(func(entry wal.Entry) error {
    return apply(entry.Data)
})
```

`Append` and `AppendBatch` sync before returning by default. For explicitly
managed batching, open with `wal.WithSyncOnWrite(false)` and call `Sync` at the
desired durability boundary. A truncated final record is discarded on open; a
checksum failure in a complete record returns `wal.ErrCorrupt`.

Run the example and tests with:

```text
go run ./examples/basic
go test ./...
```

Inspect a log without opening it for append or repairing a truncated tail:

```text
go run ./cmd/wal-inspect events.wal
```

The inspection report includes record and byte counts, the sequence range, and
the first truncated-tail or corruption issue. A committed-record corruption
is printed and returns a non-zero exit status. Interactive terminals get color
and byte-usage bars; use `-color=never` or `-color=always` to override color
detection.

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

Call `Replay` after opening to rebuild application state:

```go
err := log.Replay(func(entry wal.Entry) error {
    return apply(entry.Data)
})
```

`Append` syncs before returning by default. For batched writes, open with
`wal.WithSyncOnWrite(false)` and call `Sync` at the desired durability
boundary. A truncated final record is discarded on open; a checksum failure in
a complete record returns `wal.ErrCorrupt`.

Run the example and tests with:

```text
go run ./examples/basic
go test ./...
```

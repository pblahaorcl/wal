// Command wal-generate creates a deterministic, valid WAL fixture.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"wal"
)

func main() {
	path := flag.String("path", "huge.wal", "output WAL path")
	records := flag.Int("records", 1024, "number of records")
	payloadSize := flag.Int("payload-size", 1<<20, "payload bytes per record")
	flag.Parse()

	if *records <= 0 || *payloadSize <= 0 {
		fail("records and payload-size must be positive")
	}
	if _, err := os.Stat(*path); err == nil {
		fail("output already exists: %s", *path)
	} else if !errors.Is(err, os.ErrNotExist) {
		fail("stat output: %v", err)
	}

	log, err := wal.Open(*path,
		wal.WithSyncOnWrite(false),
		wal.WithMaxRecordSize(*payloadSize),
	)
	if err != nil {
		fail("open output: %v", err)
	}

	payload := make([]byte, *payloadSize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	for i := 0; i < *records; i++ {
		// Make each record distinguishable while keeping the fixture deterministic.
		payload[0] = byte(i)
		payload[1] = byte(i >> 8)
		if _, err := log.Append(payload); err != nil {
			_ = log.Close()
			fail("append record %d: %v", i+1, err)
		}
		if (i+1)%64 == 0 || i+1 == *records {
			fmt.Fprintf(os.Stderr, "generated %d/%d records\n", i+1, *records)
		}
	}
	if err := log.Close(); err != nil {
		fail("close output: %v", err)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "wal-generate: "+format+"\n", args...)
	os.Exit(1)
}

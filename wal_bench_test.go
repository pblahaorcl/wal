package wal

import (
	"bytes"
	"path/filepath"
	"testing"
)

var (
	benchmarkEntry    Entry
	benchmarkSequence uint64
	benchmarkReplayN  int
)

func BenchmarkAppend(b *testing.B) {
	benchmarkAppend(b, false)
}


func BenchmarkAppendSync(b *testing.B) {
	benchmarkAppend(b, true)
}

func BenchmarkAppendBatch(b *testing.B) {
	benchmarkAppendBatch(b, false)
}

func BenchmarkAppendBatchSync(b *testing.B) {
	benchmarkAppendBatch(b, true)
}

func benchmarkAppend(b *testing.B, syncOnWrite bool) {
	b.Helper()

	log, err := Open(
		filepath.Join(b.TempDir(), "append.wal"),
		WithSyncOnWrite(syncOnWrite),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := log.Close(); err != nil {
			b.Error(err)
		}
	})

	data := bytes.Repeat([]byte("x"), 256)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sequence, err := log.Append(data)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSequence = sequence
	}
}

func benchmarkAppendBatch(b *testing.B, syncOnWrite bool) {
	b.Helper()

	const batchSize = 32
	data := bytes.Repeat([]byte("x"), 256)
	batch := make([][]byte, batchSize)
	for i := range batch {
		batch[i] = data
	}

	log, err := Open(
		filepath.Join(b.TempDir(), "append-batch.wal"),
		WithSyncOnWrite(syncOnWrite),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := log.Close(); err != nil {
			b.Error(err)
		}
	})

	b.SetBytes(int64(len(data) * batchSize))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sequences, err := log.AppendBatch(batch)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkSequence = sequences[len(sequences)-1]
	}
}

func BenchmarkRead(b *testing.B) {
	const recordCount = 1024
	log := benchmarkPopulatedLog(b, recordCount, 256)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry, err := log.Read(uint64(i%recordCount) + 1)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkEntry = entry
	}
}

func BenchmarkReplay(b *testing.B) {
	const recordCount = 1024
	log := benchmarkPopulatedLog(b, recordCount, 256)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkReplayN = 0
		err := log.Replay(func(entry Entry) error {
			benchmarkSequence = entry.Sequence
			benchmarkEntry = entry
			benchmarkReplayN++
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkOpen(b *testing.B) {
	const recordCount = 1024
	path := filepath.Join(b.TempDir(), "open.wal")

	log, err := Open(path, WithSyncOnWrite(false))
	if err != nil {
		b.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), 256)
	for i := 0; i < recordCount; i++ {
		if _, err := log.Append(data); err != nil {
			b.Fatal(err)
		}
	}
	if err := log.Close(); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opened, err := Open(path, WithSyncOnWrite(false))
		if err != nil {
			b.Fatal(err)
		}
		if err := opened.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkPopulatedLog(b *testing.B, recordCount, payloadSize int) *Log {
	b.Helper()

	log, err := Open(
		filepath.Join(b.TempDir(), "populated.wal"),
		WithSyncOnWrite(false),
	)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := log.Close(); err != nil {
			b.Error(err)
		}
	})

	data := bytes.Repeat([]byte("x"), payloadSize)
	for i := 0; i < recordCount; i++ {
		if _, err := log.Append(data); err != nil {
			b.Fatal(err)
		}
	}
	return log
}

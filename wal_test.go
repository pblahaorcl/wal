package wal

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAppendReadReplayAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	first, err := log.Append([]byte("create account"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := log.Append([]byte("credit 25"))
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 2 {
		t.Fatalf("sequences = %d, %d; want 1, 2", first, second)
	}

	entry, err := log.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Sequence != 2 || string(entry.Data) != "credit 25" {
		t.Fatalf("entry = %#v", entry)
	}
	entry.Data[0] = 'X'
	entry, err = log.Read(2)
	if err != nil {
		t.Fatal(err)
	}
	if string(entry.Data) != "credit 25" {
		t.Fatal("Read did not return a copy of the payload")
	}

	var replay []string
	if err := log.Replay(func(entry Entry) error {
		replay = append(replay, string(entry.Data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"create account", "credit 25"}; !reflect.DeepEqual(replay, want) {
		t.Fatalf("replay = %v; want %v", replay, want)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	log, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	third, err := log.Append([]byte("debit 5"))
	if err != nil {
		t.Fatal(err)
	}
	if third != 3 {
		t.Fatalf("third sequence = %d; want 3", third)
	}
}

func TestAppendBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	data := [][]byte{[]byte("one"), nil, []byte("three")}
	sequences, err := log.AppendBatch(data)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint64{1, 2, 3}; !reflect.DeepEqual(sequences, want) {
		t.Fatalf("sequences = %v; want %v", sequences, want)
	}

	data[0][0] = 'X'
	data[2] = []byte("changed")
	for sequence, want := range map[uint64]string{1: "one", 2: "", 3: "three"} {
		entry, err := log.Read(sequence)
		if err != nil {
			t.Fatal(err)
		}
		if string(entry.Data) != want {
			t.Fatalf("Read(%d) = %q; want %q", sequence, entry.Data, want)
		}
	}

	empty, err := log.AppendBatch(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty batch sequences = %v; want no sequences", empty)
	}

	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	log, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	sequence, err := log.AppendBatch([][]byte{[]byte("four"), []byte("five")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []uint64{4, 5}; !reflect.DeepEqual(sequence, want) {
		t.Fatalf("sequences after reopen = %v; want %v", sequence, want)
	}
}

func TestAppendBatchValidatesBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path, WithMaxRecordSize(3), WithSyncOnWrite(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	if _, err := log.Append([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if _, err := log.AppendBatch([][]byte{[]byte("yes"), []byte("long")}); err == nil {
		t.Fatal("AppendBatch accepted a record larger than the configured maximum")
	}
	if _, err := log.Read(2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(2) after rejected batch = %v; want ErrNotFound", err)
	}
	sequence, err := log.Append([]byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	if sequence != 2 {
		t.Fatalf("sequence after rejected batch = %d; want 2", sequence)
	}
}

func TestOpenRecoversIncompleteTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append([]byte("two")); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("WAL1\x00\x00")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	log, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if _, err := log.Read(3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Read(3) error = %v; want ErrNotFound", err)
	}
	sequence, err := log.Append([]byte("three"))
	if err != nil {
		t.Fatal(err)
	}
	if sequence != 3 {
		t.Fatalf("sequence after recovery = %d; want 3", sequence)
	}
}

func TestOpenRejectsCorruptRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append([]byte("important")); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("X"), recordHeaderSize); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(path)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open error = %v; want ErrCorrupt", err)
	}
}

func TestReplayStopsOnCallbackError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	for _, value := range []string{"one", "two", "three"} {
		if _, err := log.Append([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}

	wantErr := errors.New("stop")
	called := 0
	err = log.Replay(func(Entry) error {
		called++
		return wantErr
	})
	if !errors.Is(err, wantErr) || called != 1 {
		t.Fatalf("Replay() error, calls = %v, %d; want %v, 1", err, called, wantErr)
	}
}

func TestOptionsAndClosedLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path, WithSyncOnWrite(false), WithMaxRecordSize(3))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append([]byte("four")); err == nil {
		t.Fatal("Append accepted a record larger than the configured maximum")
	}
	if _, err := log.Append([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append([]byte("no")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Append after Close() = %v; want ErrClosed", err)
	}
	if _, err := log.AppendBatch([][]byte{[]byte("no")}); !errors.Is(err, ErrClosed) {
		t.Fatalf("AppendBatch after Close() = %v; want ErrClosed", err)
	}
	if err := log.Sync(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Sync after Close() = %v; want ErrClosed", err)
	}
}

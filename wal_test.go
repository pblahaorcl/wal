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
	if err := log.Sync(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Sync after Close() = %v; want ErrClosed", err)
	}
}

package wal

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectReportsUsageAndSequenceRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.AppendBatch([][]byte{[]byte("one"), []byte("two")}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	report, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.RecordCount != 2 || report.FirstSequence != 1 || report.LastSequence != 2 {
		t.Fatalf("record/sequence report = %#v", report)
	}
	if report.FileBytes != 46 || report.RecordBytes != 46 || report.HeaderBytes != 40 || report.PayloadBytes != 6 || report.UnparsedBytes != 0 {
		t.Fatalf("byte report = %#v", report)
	}
	if report.Issue != nil {
		t.Fatalf("issue = %#v; want none", report.Issue)
	}
}

func TestInspectReportsTruncatedTailWithoutRepairing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.wal")
	log, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Append([]byte("one")); err != nil {
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

	report, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Issue == nil || report.Issue.Kind != InspectionTruncatedTail || report.Issue.Offset != 23 {
		t.Fatalf("issue = %#v", report.Issue)
	}
	if report.RecordCount != 1 || report.UnparsedBytes != 6 {
		t.Fatalf("report = %#v", report)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 29 {
		t.Fatalf("Inspect changed file size to %d; want 29", info.Size())
	}
}

func TestInspectReportsCorruptionDetails(t *testing.T) {
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

	report, err := Inspect(path)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Inspect error = %v; want ErrCorrupt", err)
	}
	if report.Issue == nil || report.Issue.Kind != InspectionCorruption || report.Issue.Offset != 0 || report.Issue.Reason != "checksum mismatch" {
		t.Fatalf("issue = %#v", report.Issue)
	}
	if report.RecordCount != 0 || report.UnparsedBytes != report.FileBytes {
		t.Fatalf("report = %#v", report)
	}
}

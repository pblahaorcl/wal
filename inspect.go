package wal

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

// Inspection describes the contents of a WAL file without opening it for
// append or repairing it. Byte counts after the first issue describe the
// valid record prefix; UnparsedBytes accounts for the rest of the file.
type Inspection struct {
	FileBytes     uint64
	RecordBytes   uint64
	HeaderBytes   uint64
	PayloadBytes  uint64
	UnparsedBytes uint64
	RecordCount   uint64
	FirstSequence uint64
	LastSequence  uint64
	Issue         *InspectionIssue
}

// InspectionIssueKind identifies an issue found after the valid record
// prefix. A truncated tail is recoverable by Open; corruption is not.
type InspectionIssueKind string

const (
	InspectionTruncatedTail InspectionIssueKind = "truncated tail"
	InspectionCorruption    InspectionIssueKind = "corruption"
)

// InspectionIssue contains the first issue found while scanning a WAL file.
type InspectionIssue struct {
	Offset uint64
	Kind   InspectionIssueKind
	Reason string
}

// Inspect scans path read-only and returns record counts, byte usage, the
// sequence range, and the first issue, if any. A truncated final record is
// reported in Inspection.Issue and does not cause an error because Open would
// recover it. Committed-record corruption is reported in Inspection.Issue and
// returned as an error wrapping ErrCorrupt.
func Inspect(path string, optionFns ...Option) (Inspection, error) {
	opts, err := resolveOptions(optionFns)
	if err != nil {
		return Inspection{}, err
	}

	f, err := os.Open(path)
	if err != nil {
		return Inspection{}, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return Inspection{}, err
	}
	if info.Size() < 0 {
		return Inspection{}, fmt.Errorf("wal: negative file size: %d", info.Size())
	}

	report := Inspection{FileBytes: uint64(info.Size())}
	offset := uint64(0)
	expectedSequence := uint64(1)
	for offset < report.FileBytes {
		remaining := report.FileBytes - offset
		if remaining < recordHeaderSize {
			setInspectionIssue(&report, offset, InspectionTruncatedTail,
				fmt.Sprintf("incomplete header: %d of %d bytes", remaining, recordHeaderSize))
			break
		}

		header := make([]byte, recordHeaderSize)
		if err := readAtFull(f, header, offset); err != nil {
			return report, fmt.Errorf("wal: read header at offset %d: %w", offset, err)
		}
		if string(header[:4]) != recordMagic {
			return report, reportCorruption(&report, offset,
				fmt.Sprintf("invalid record magic %x", header[:4]))
		}

		sequence := binary.BigEndian.Uint64(header[4:12])
		length := binary.BigEndian.Uint32(header[12:16])
		checksum := binary.BigEndian.Uint32(header[16:20])
		if sequence != expectedSequence {
			return report, reportCorruption(&report, offset,
				fmt.Sprintf("expected sequence %d, got %d", expectedSequence, sequence))
		}
		if uint64(length) > uint64(opts.maxRecord) {
			return report, reportCorruption(&report, offset,
				fmt.Sprintf("record size %d exceeds maximum %d", length, opts.maxRecord))
		}

		recordSize := uint64(recordHeaderSize) + uint64(length)
		if recordSize > remaining {
			setInspectionIssue(&report, offset, InspectionTruncatedTail,
				fmt.Sprintf("declared record size %d, only %d bytes remain", recordSize, remaining))
			break
		}

		data := make([]byte, length)
		if err := readAtFull(f, data, offset+recordHeaderSize); err != nil {
			return report, fmt.Errorf("wal: read record at offset %d: %w", offset, err)
		}
		if crc32.ChecksumIEEE(data) != checksum {
			return report, reportCorruption(&report, offset, "checksum mismatch")
		}

		report.RecordCount++
		report.HeaderBytes += recordHeaderSize
		report.PayloadBytes += uint64(length)
		report.RecordBytes += recordSize
		if report.FirstSequence == 0 {
			report.FirstSequence = sequence
		}
		report.LastSequence = sequence
		offset += recordSize
		expectedSequence++
	}

	report.UnparsedBytes = report.FileBytes - report.RecordBytes
	return report, nil
}

func setInspectionIssue(report *Inspection, offset uint64, kind InspectionIssueKind, reason string) {
	report.Issue = &InspectionIssue{Offset: offset, Kind: kind, Reason: reason}
}

func reportCorruption(report *Inspection, offset uint64, reason string) error {
	setInspectionIssue(report, offset, InspectionCorruption, reason)
	report.UnparsedBytes = report.FileBytes - report.RecordBytes
	return corrupt(offset, reason)
}

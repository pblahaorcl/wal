// Package wal provides a small, durable write-ahead log.
//
// A Log stores records in append order. Each record has a monotonically
// increasing sequence number and a CRC-32 checksum. By default Append and
// AppendBatch sync the file before returning, so a successful append is
// durable according to the operating system's fsync semantics.
package wal

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
)

const (
	recordMagic      = "WAL1"
	recordHeaderSize = 20 // magic (4), sequence (8), length (4), checksum (4)
	defaultMaxRecord = 64 << 20
)

var (
	// ErrClosed is returned when an operation is attempted on a closed Log.
	ErrClosed = errors.New("wal: log is closed")
	// ErrNotFound is returned when a requested sequence number does not exist.
	ErrNotFound = errors.New("wal: entry not found")
	// ErrCorrupt is returned when a committed record cannot be validated.
	ErrCorrupt = errors.New("wal: corrupt log")
)

// Entry is a record stored in a Log.
type Entry struct {
	Sequence uint64
	Data     []byte
}

type options struct {
	syncOnWrite bool
	fileMode    os.FileMode
	maxRecord   int
}

// Option configures a Log opened by Open.
type Option func(*options)

// WithSyncOnWrite controls whether Append and AppendBatch call File.Sync before
// returning. It defaults to true. Set it to false when managing durability
// explicitly; call Sync before depending on those writes for crash durability.
func WithSyncOnWrite(enabled bool) Option {
	return func(o *options) { o.syncOnWrite = enabled }
}

// WithFileMode sets the permissions used when the log file is created. The
// default is 0600. It has no effect when opening an existing file.
func WithFileMode(mode os.FileMode) Option {
	return func(o *options) { o.fileMode = mode }
}

// WithMaxRecordSize sets the maximum payload size accepted by Append and
// Open. The default is 64 MiB.
func WithMaxRecordSize(size int) Option {
	return func(o *options) { o.maxRecord = size }
}

type recordMeta struct {
	offset uint64
	length uint32
}

// Log is an append-only write-ahead log backed by a single file.
//
// A Log is safe for concurrent use. Data returned by Read and Replay is copied
// and can be modified by the caller.
type Log struct {
	mu      sync.Mutex
	file    *os.File
	offset  uint64
	nextSeq uint64
	records []recordMeta
	opts    options
	closed  bool
}

// Open opens or creates the log at path. Existing records are validated while
// opening. An incomplete final record is treated as a torn write and removed;
// corruption in a complete record is returned as ErrCorrupt.
func Open(path string, optionFns ...Option) (*Log, error) {
	opts, err := resolveOptions(optionFns)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, opts.fileMode)
	if err != nil {
		return nil, err
	}

	l := &Log{file: f, nextSeq: 1, opts: opts}
	if err := l.load(); err != nil {
		_ = f.Close()
		return nil, err
	}
	return l, nil
}

func resolveOptions(optionFns []Option) (options, error) {
	opts := options{
		syncOnWrite: true,
		fileMode:    0600,
		maxRecord:   defaultMaxRecord,
	}
	for _, optionFn := range optionFns {
		if optionFn != nil {
			optionFn(&opts)
		}
	}
	if opts.maxRecord <= 0 {
		return options{}, fmt.Errorf("wal: max record size must be positive: %d", opts.maxRecord)
	}
	if uint64(opts.maxRecord) > uint64(^uint32(0)) {
		return options{}, fmt.Errorf("wal: max record size exceeds on-disk limit: %d", opts.maxRecord)
	}
	return opts, nil
}

// Append adds data to the log and returns its sequence number. Append copies
// data before returning, so the caller may safely reuse its input buffer.
func (l *Log) Append(data []byte) (uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	sequence, err := l.appendBatchLocked([][]byte{data})
	return sequence, err
}

// AppendBatch adds each payload to the log and returns the assigned sequence
// numbers in the same order as data. The payloads are copied before returning,
// so the caller may safely reuse its input buffers. When sync-on-write is
// enabled, all records are written first and then made durable with one sync.
// An empty batch is a no-op.
func (l *Log) AppendBatch(data [][]byte) ([]uint64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	firstSequence, err := l.appendBatchLocked(data)
	if err != nil {
		return nil, err
	}

	sequences := make([]uint64, len(data))
	for i := range sequences {
		sequences[i] = firstSequence + uint64(i)
	}
	return sequences, nil
}

func (l *Log) appendBatchLocked(data [][]byte) (uint64, error) {
	if err := l.ensureOpen(); err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return l.nextSeq, nil
	}
	if l.nextSeq == 0 {
		return 0, errors.New("wal: sequence number exhausted")
	}
	availableSequences := ^uint64(0) - l.nextSeq + 1
	if uint64(len(data)) > availableSequences {
		return 0, errors.New("wal: sequence number exhausted")
	}
	for _, payload := range data {
		if len(payload) > l.opts.maxRecord {
			return 0, fmt.Errorf("wal: record size %d exceeds maximum %d", len(payload), l.opts.maxRecord)
		}
	}

	firstSequence := l.nextSeq
	writeOffset := l.offset
	metas := make([]recordMeta, len(data))
	for i, payload := range data {
		sequence := firstSequence + uint64(i)
		record := makeRecord(sequence, payload)
		if err := writeAtFull(l.file, record, writeOffset); err != nil {
			if truncateErr := l.file.Truncate(int64(l.offset)); truncateErr != nil {
				return 0, fmt.Errorf("wal: write record: %w (truncate partial batch: %v)", err, truncateErr)
			}
			return 0, err
		}
		metas[i] = recordMeta{offset: writeOffset, length: uint32(len(payload))}
		writeOffset += uint64(len(record))
	}

	if l.opts.syncOnWrite {
		if err := l.file.Sync(); err != nil {
			if truncateErr := l.file.Truncate(int64(l.offset)); truncateErr != nil {
				return 0, fmt.Errorf("wal: sync batch: %w (truncate unsynced batch: %v)", err, truncateErr)
			}
			return 0, err
		}
	}

	l.records = append(l.records, metas...)
	l.offset = writeOffset
	l.nextSeq = firstSequence + uint64(len(data))
	return firstSequence, nil
}

// Read returns the entry with sequence. Sequences start at one.
func (l *Log) Read(sequence uint64) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.ensureOpen(); err != nil {
		return Entry{}, err
	}
	meta, ok := l.meta(sequence)
	if !ok {
		return Entry{}, ErrNotFound
	}
	data, err := l.readEntry(meta, sequence)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Sequence: sequence, Data: data}, nil
}

// Replay calls fn once for every entry currently in the log, in sequence
// order. Replay observes a snapshot of the end of the log: appends performed
// by fn or another goroutine are not included in the current replay.
func (l *Log) Replay(fn func(Entry) error) error {
	if fn == nil {
		return errors.New("wal: replay callback is nil")
	}

	l.mu.Lock()
	if err := l.ensureOpen(); err != nil {
		l.mu.Unlock()
		return err
	}
	records := append([]recordMeta(nil), l.records...)
	l.mu.Unlock()

	for i, meta := range records {
		l.mu.Lock()
		if err := l.ensureOpen(); err != nil {
			l.mu.Unlock()
			return err
		}
		entry, err := l.readEntry(meta, uint64(i+1))
		l.mu.Unlock()
		if err != nil {
			return err
		}
		if err := fn(Entry{Sequence: uint64(i + 1), Data: entry}); err != nil {
			return err
		}
	}
	return nil
}

// Sync flushes buffered log data to stable storage according to the operating
// system's File.Sync semantics.
func (l *Log) Sync() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.ensureOpen(); err != nil {
		return err
	}
	return l.file.Sync()
}

// Close syncs and closes the log. Close is idempotent.
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return nil
	}
	if err := l.file.Sync(); err != nil {
		_ = l.file.Close()
		l.closed = true
		return err
	}
	err := l.file.Close()
	l.closed = true
	return err
}

func (l *Log) load() error {
	info, err := l.file.Stat()
	if err != nil {
		return err
	}

	fileSize := uint64(info.Size())
	offset := uint64(0)
	expectedSequence := uint64(1)
	var records []recordMeta
	for offset < fileSize {
		remaining := fileSize - offset
		if remaining < recordHeaderSize {
			l.records = records
			return l.recoverTail(offset)
		}

		header := make([]byte, recordHeaderSize)
		if err := readAtFull(l.file, header, offset); err != nil {
			return fmt.Errorf("wal: read header at offset %d: %w", offset, err)
		}
		if string(header[:4]) != recordMagic {
			return corrupt(offset, "invalid record magic")
		}
		sequence := binary.BigEndian.Uint64(header[4:12])
		length := binary.BigEndian.Uint32(header[12:16])
		checksum := binary.BigEndian.Uint32(header[16:20])
		if sequence != expectedSequence {
			return corrupt(offset, fmt.Sprintf("expected sequence %d, got %d", expectedSequence, sequence))
		}
		if uint64(length) > uint64(l.opts.maxRecord) {
			return corrupt(offset, fmt.Sprintf("record size %d exceeds maximum %d", length, l.opts.maxRecord))
		}

		recordSize := uint64(recordHeaderSize) + uint64(length)
		if recordSize > remaining {
			l.records = records
			return l.recoverTail(offset)
		}
		data := make([]byte, length)
		if err := readAtFull(l.file, data, offset+recordHeaderSize); err != nil {
			return fmt.Errorf("wal: read record at offset %d: %w", offset, err)
		}
		if crc32.ChecksumIEEE(data) != checksum {
			return corrupt(offset, "checksum mismatch")
		}

		records = append(records, recordMeta{offset: offset, length: length})
		offset += recordSize
		expectedSequence++
	}

	l.records = records
	l.offset = offset
	l.nextSeq = expectedSequence
	return nil
}

func (l *Log) recoverTail(offset uint64) error {
	if err := l.file.Truncate(int64(offset)); err != nil {
		return fmt.Errorf("wal: truncate incomplete tail: %w", err)
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("wal: sync recovered log: %w", err)
	}
	l.offset = offset
	l.nextSeq = uint64(len(l.records)) + 1
	return nil
}

func (l *Log) readEntry(meta recordMeta, sequence uint64) ([]byte, error) {
	data := make([]byte, meta.length)
	if err := readAtFull(l.file, data, meta.offset+recordHeaderSize); err != nil {
		return nil, fmt.Errorf("wal: read sequence %d: %w", sequence, err)
	}
	header := make([]byte, recordHeaderSize)
	if err := readAtFull(l.file, header, meta.offset); err != nil {
		return nil, fmt.Errorf("wal: read sequence %d header: %w", sequence, err)
	}
	if string(header[:4]) != recordMagic || binary.BigEndian.Uint64(header[4:12]) != sequence ||
		binary.BigEndian.Uint32(header[12:16]) != meta.length || crc32.ChecksumIEEE(data) != binary.BigEndian.Uint32(header[16:20]) {
		return nil, corrupt(meta.offset, "record validation failed")
	}
	return data, nil
}

func (l *Log) meta(sequence uint64) (recordMeta, bool) {
	if sequence == 0 || sequence > uint64(len(l.records)) {
		return recordMeta{}, false
	}
	return l.records[sequence-1], true
}

func (l *Log) ensureOpen() error {
	if l.closed || l.file == nil {
		return ErrClosed
	}
	return nil
}

func makeRecord(sequence uint64, data []byte) []byte {
	record := make([]byte, recordHeaderSize+len(data))
	copy(record[:4], recordMagic)
	binary.BigEndian.PutUint64(record[4:12], sequence)
	binary.BigEndian.PutUint32(record[12:16], uint32(len(data)))
	binary.BigEndian.PutUint32(record[16:20], crc32.ChecksumIEEE(data))
	copy(record[recordHeaderSize:], data)
	return record
}

func writeAtFull(f *os.File, data []byte, offset uint64) error {
	for len(data) > 0 {
		n, err := f.WriteAt(data, int64(offset))
		if n > 0 {
			data = data[n:]
			offset += uint64(n)
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func readAtFull(f *os.File, data []byte, offset uint64) error {
	for len(data) > 0 {
		n, err := f.ReadAt(data, int64(offset))
		if n > 0 {
			data = data[n:]
			offset += uint64(n)
		}
		if err != nil {
			if err == io.EOF && len(data) == 0 {
				return nil
			}
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}

func corrupt(offset uint64, reason string) error {
	return fmt.Errorf("%w at offset %d: %s", ErrCorrupt, offset, reason)
}

// Package tail reads complete JSONL lines using persistent file offsets.
package tail

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/labstack/driga/internal/model"
)

// MaxLine is the maximum line length in bytes, excluding the newline.
const MaxLine = 4 << 20

const bufferSize = 64 << 10

// Result contains complete lines and the offset to persist for the next read.
type Result struct {
	Lines   [][]byte
	Next    model.Offset
	Skipped int
	Reset   bool
}

// Read returns complete lines without their newline. maxBytes bounds consumed
// bytes (offset advancement); a first line of at most MaxLine may exceed that
// budget to ensure progress. A nonpositive budget consumes nothing.
//
// Boundary scans may look ahead and accepted lines are reread into owned slices.
// Oversized lines are scanned with a fixed buffer, never accumulated. Complete
// oversized lines can be consumed across calls, counting Skipped at the newline.
// A partial line, even an oversized one, leaves Next.Pos at its start and is
// rescanned on the next call. File size and mtime describe the opening snapshot.
func Read(path string, prev model.Offset, maxBytes int64) (Result, error) {
	return read(path, prev, maxBytes, nil, nil)
}

// Two reusable line buffers bound concurrent streaming reads across adapters to
// 2 * MaxLine, rather than one entire read budget for each parsing worker.
var scanBuffers = func() chan []byte {
	pool := make(chan []byte, 2)
	pool <- nil
	pool <- nil
	return pool
}()

// Scan folds complete lines with their absolute offsets. The callback must not
// retain line, which is reused on the next call. Result.Lines remains empty;
// offsets, budgets, oversized records and partial lines follow Read semantics.
func Scan(ctx context.Context, path string, prev model.Offset, maxBytes int64, visit func(line []byte, offset int64)) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	var buffer []byte
	select {
	case buffer = <-scanBuffers:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	defer func() { scanBuffers <- buffer }()
	return read(path, prev, maxBytes, &buffer, func(line []byte, offset int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		visit(line, offset)
		return nil
	})
}

func read(path string, prev model.Offset, maxBytes int64, buffer *[]byte, visit func([]byte, int64) error) (Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()
	next, err := fileOffset(f)
	if err != nil {
		return Result{}, err
	}
	r := Result{Next: next}
	r.Reset = next.Inode != prev.Inode || next.Size < prev.Pos
	if !r.Reset {
		r.Next.Pos = prev.Pos
	}
	if r.Next.Pos < 0 {
		return r, fmt.Errorf("negative tail offset: %d", r.Next.Pos)
	}
	if maxBytes <= 0 || r.Next.Pos == next.Size {
		return r, nil
	}
	start := r.Next.Pos
	pos := start
	// Only complete oversized lines produce offsets within a line. Detect their
	// continuation so no suffix can be mistaken for a new JSONL record.
	continuation := false
	if pos > 0 {
		var previous [1]byte
		if _, err := f.ReadAt(previous[:], pos-1); err != nil {
			return r, err
		}
		continuation = previous[0] != '\n'
	}
	reader := bufio.NewReaderSize(io.NewSectionReader(f, pos, next.Size-pos), bufferSize)
	for pos < next.Size && pos-start < maxBytes {
		end, complete, err := scanLine(reader, pos)
		if err != nil {
			return r, err
		}
		if !complete {
			if continuation {
				// The file may have been truncated inside a previously complete
				// oversized record. Rewind to its true start rather than its suffix.
				r.Next.Pos, err = lineStart(f, pos)
			}
			return r, err
		}
		remaining := maxBytes - (pos - start)
		if continuation || end-pos-1 > MaxLine {
			// Discard without allocating a line-sized slice. Preserve budget
			// progress within a known complete oversized line if necessary.
			if end-pos > remaining {
				r.Next.Pos = pos + remaining
				return r, nil
			}
			r.Skipped++
		} else {
			if end-pos > remaining && pos != start {
				return r, nil
			}
			size := int(end - pos - 1)
			var line []byte
			if buffer == nil {
				line = make([]byte, size)
			} else {
				if cap(*buffer) < size {
					*buffer = make([]byte, size)
				}
				line = (*buffer)[:size]
			}
			if _, err := f.ReadAt(line, pos); err != nil {
				return r, err
			}
			if visit == nil {
				r.Lines = append(r.Lines, line)
			} else if err := visit(line, pos); err != nil {
				return r, err
			}
		}
		pos = end
		r.Next.Pos = pos
		continuation = false
	}
	return r, nil
}

// scanLine finds the next boundary without retaining fragments of the line.
func scanLine(reader *bufio.Reader, pos int64) (end int64, complete bool, err error) {
	end = pos
	for {
		fragment, readErr := reader.ReadSlice('\n')
		end += int64(len(fragment))
		switch readErr {
		case nil:
			return end, true, nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			return end, false, nil
		default:
			return end, false, readErr
		}
	}
}

// SeekEnd establishes a baseline at EOF, or at the start of a trailing partial
// line so that record is returned once its terminating newline is appended.
func SeekEnd(path string) (model.Offset, error) {
	f, err := os.Open(path)
	if err != nil {
		return model.Offset{}, err
	}
	defer f.Close()
	o, err := fileOffset(f)
	if err != nil {
		return model.Offset{}, err
	}
	o.Pos, err = lineStart(f, o.Size)
	return o, err
}

// SeekRecent skips a backlog larger than maxBytes to the first line boundary
// at or after size-maxBytes. It returns the skipped byte count, measured from
// the previous position (or zero after rotation/truncation). A trailing partial
// line is retained at its start until its newline arrives. Read is unchanged.
func SeekRecent(path string, prev model.Offset, maxBytes int64) (model.Offset, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return prev, 0, err
	}
	defer f.Close()
	o, err := fileOffset(f)
	if err != nil {
		return prev, 0, err
	}
	start := prev.Pos
	if o.Inode != prev.Inode || o.Size < start {
		start = 0
	}
	if start < 0 {
		return prev, 0, fmt.Errorf("negative tail offset: %d", start)
	}
	if maxBytes <= 0 || o.Size-start <= maxBytes {
		return prev, 0, nil
	}
	o.Pos = o.Size - maxBytes
	var before [1]byte
	if _, err := f.ReadAt(before[:], o.Pos-1); err != nil {
		return prev, 0, err
	}
	if before[0] != '\n' {
		reader := bufio.NewReaderSize(io.NewSectionReader(f, o.Pos, o.Size-o.Pos), bufferSize)
		end, complete, err := scanLine(reader, o.Pos)
		if err != nil {
			return prev, 0, err
		}
		if complete {
			o.Pos = end
		} else {
			// No later boundary yet: retain the incomplete record for Read.
			o.Pos, err = lineStart(f, o.Size)
			if err != nil {
				return prev, 0, err
			}
		}
	}
	if o.Pos <= start {
		return prev, 0, nil
	}
	return o, o.Pos - start, nil
}

func fileOffset(f *os.File) (model.Offset, error) {
	info, err := f.Stat()
	if err != nil {
		return model.Offset{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return model.Offset{}, fmt.Errorf("inode unavailable for %s", f.Name())
	}
	return model.Offset{Inode: stat.Ino, Size: info.Size(), Mtime: info.ModTime()}, nil
}

// lineStart searches backwards in bounded chunks, returning the byte after the
// last newline before end, or zero if the file has no earlier newline.
func lineStart(f *os.File, end int64) (int64, error) {
	var buffer [bufferSize]byte
	for end > 0 {
		start := max(int64(0), end-int64(len(buffer)))
		chunk := buffer[:int(end-start)]
		if _, err := f.ReadAt(chunk, start); err != nil {
			return 0, err
		}
		if i := bytes.LastIndexByte(chunk, '\n'); i >= 0 {
			return start + int64(i) + 1, nil
		}
		end = start
	}
	return 0, nil
}

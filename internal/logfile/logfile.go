// Package logfile bounds append-only logs while preserving inherited service FDs.
package logfile

import (
	"errors"
	"github.com/labstack/driga/internal/filelock"
	"github.com/labstack/driga/internal/secfile"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

const MaxSize int64 = 10 << 20

// TextHandler keeps collector/hook diagnostic fields and UTC instants consistent.
func TextHandler(w io.Writer) *slog.TextHandler {
	return slog.NewTextHandler(w, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
			a.Value = slog.TimeValue(a.Value.Time().UTC())
		}
		return a
	}})
}

// Writer keeps at most MaxSize bytes plus one bounded .1 file. Rotation copies
// the old tail and truncates in place: launchd's open stdout/stderr descriptors
// continue to refer to the current log rather than an unlinked rotated inode.
type Writer struct {
	Path string
	mu   sync.Mutex
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := secfile.MkdirPrivate(filepath.Dir(w.Path)); err != nil {
		return 0, err
	}
	flags := os.O_CREATE | os.O_APPEND | os.O_RDWR
	if runtime.GOOS == "windows" {
		// O_APPEND drops FILE_WRITE_DATA on Windows, which Truncate needs.
		// The file lock serializes seeking and writing with other Writers.
		flags &^= os.O_APPEND
	}
	f, err := secfile.OpenFile(w.Path, flags)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := filelock.Lock(f, false); err != nil {
		return 0, err
	}
	defer filelock.Unlock(f)
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	original := len(p)
	if int64(len(p)) > MaxSize {
		p = p[len(p)-int(MaxSize):]
	}
	if info.Size()+int64(len(p)) > MaxSize {
		old, err := secfile.OpenFile(w.Path+".1", os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
		if err != nil {
			return 0, err
		}
		_, copyErr := io.Copy(old, io.NewSectionReader(f, max(0, info.Size()-MaxSize), min(info.Size(), MaxSize)))
		if err := errors.Join(copyErr, old.Close()); err != nil {
			return 0, err
		}
		if err := f.Truncate(0); err != nil {
			return 0, err
		}
	}
	if runtime.GOOS == "windows" {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return 0, err
		}
	}
	n, err := f.Write(p)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return original, nil
}

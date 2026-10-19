// Package spool appends hook events without opening the collector's database.
package spool

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/tail"
)

const maxAppendLine = 4 << 10

// Append writes one bounded JSON line with a single O_APPEND write. The event's
// UTC date determines its file, so delayed events retain their original day.
func Append(dir string, ev model.HookEvent) error { return AppendAll(dir, []model.HookEvent{ev}) }

// AppendAll encodes every event before writing any, then gives each day file
// its lines in one O_APPEND write, so an encoding error leaves no partial batch.
func AppendAll(dir string, evs []model.HookEvent) error {
	files := map[string][]byte{}
	var order []string
	for _, ev := range evs {
		line, err := appendLine(ev)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, ev.TS.UTC().Format("20060102")+".jsonl")
		if _, ok := files[path]; !ok {
			order = append(order, path)
		}
		files[path] = append(files[path], line...)
	}
	if len(order) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	for _, path := range order {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			return err
		}
		n, writeErr := f.Write(files[path])
		if writeErr == nil && n != len(files[path]) {
			writeErr = io.ErrShortWrite
		}
		if err := errors.Join(writeErr, f.Close()); err != nil {
			return err
		}
	}
	// Appending leaves directory mtime unchanged; wake the collector with one
	// directory stat even when writing an existing day file.
	now := time.Now()
	return os.Chtimes(dir, now, now)
}

func appendLine(ev model.HookEvent) ([]byte, error) {
	// First bound the input to the most runes that could possibly fit. Binary
	// search measures JSON escaping as well as UTF-8 bytes and the newline.
	end := min(len(ev.Message), maxAppendLine)
	for end > 0 && end < len(ev.Message) && !utf8.RuneStart(ev.Message[end]) {
		end--
	}
	ev.Message = ev.Message[:end]
	line, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	if len(line)+1 <= maxAppendLine {
		return append(line, '\n'), nil
	}
	runes := []rune(ev.Message)
	ev.Message = ""
	line, err = json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	if len(line)+1 > maxAppendLine {
		return nil, fmt.Errorf("hook event metadata exceeds %d bytes", maxAppendLine)
	}
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		ev.Message = string(runes[:mid])
		candidate, err := json.Marshal(ev)
		if err != nil {
			return nil, err
		}
		if len(candidate)+1 <= maxAppendLine {
			lo = mid
			line = candidate
		} else {
			hi = mid - 1
		}
	}
	return append(line, '\n'), nil
}

// Drain reads complete JSONL records using base-name offsets. An optional since
// skips day files before its UTC date.
// It copies off and skips malformed records. Missing directories are empty.
// Rotation, truncation and trailing partial lines follow tail.Read semantics.
func Drain(dir string, off map[string]model.Offset, since ...time.Time) ([]model.HookEvent, map[string]model.Offset, error) {
	next := make(map[string]model.Offset, len(off))
	for name, o := range off {
		next[name] = o
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, next, nil
	}
	if err != nil {
		return nil, next, err
	}
	var events []model.HookEvent
	var errs []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		if len(since) > 0 && !RecentDay(entry.Name(), since[0]) {
			continue
		}
		result, err := tail.Read(filepath.Join(dir, entry.Name()), off[entry.Name()], math.MaxInt64)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		next[entry.Name()] = result.Next
		for _, line := range result.Lines {
			var ev *model.HookEvent
			if err := json.Unmarshal(line, &ev); err == nil && ev != nil {
				events = append(events, *ev)
			}
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].TS.Before(events[j].TS) })
	return events, next, errors.Join(errs...)
}

// Prune removes day files older than 30 days, retaining the cutoff day.
func Prune(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() && !RecentDay(entry.Name(), now.AddDate(0, 0, -30)) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// RecentDay includes the cutoff's UTC day and leaves unknown filenames alone.
func RecentDay(name string, since time.Time) bool {
	day, err := time.Parse("20060102.jsonl", strings.TrimPrefix(name, "legacy-"))
	if err != nil || since.IsZero() {
		return true
	}
	y, m, d := since.UTC().Date()
	return !day.Before(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
}

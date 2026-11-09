// Package reclaim stores local action receipts without command lines, paths or
// error text. Memory figures describe exited processes, not free system RAM.
package reclaim

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/logfile"
)

const maxReceipt = 64 << 10

type Entry struct {
	At              time.Time          `json:"occurred_at"`
	StartedAt       time.Time          `json:"started_at,omitempty"`
	Kind            string             `json:"kind"`
	ID              string             `json:"id"`
	MachineID       string             `json:"machine_id,omitempty"`
	RootPID         int32              `json:"root_pid,omitempty"`
	Status          string             `json:"status"`
	ErrorCode       string             `json:"error_code,omitempty"`
	Signaled        []int32            `json:"signaled,omitempty"`
	Stopped         []int32            `json:"stopped,omitempty"`
	Survivors       []int32            `json:"survivors,omitempty"`
	Unknown         []int32            `json:"unknown,omitempty"`
	Before          derive.MemoryUsage `json:"before"`
	StoppedUsage    derive.MemoryUsage `json:"stopped_usage"`
	LastObservedCPU *float64           `json:"last_observed_cpu_percent,omitempty"`
	FreedBytes      int64              `json:"freed_bytes,omitempty"`
	DryRun          bool               `json:"dry_run,omitempty"`
}

type History struct {
	Entries  []Entry  `json:"entries"`
	Warnings []string `json:"warnings,omitempty"`
}

// Append uses the shared cross-process locked, bounded log writer (10 MiB plus
// one rotated file), synchronizing receipts while their file lock is held.
func Append(dir string, entry Entry) error {
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if len(b) > maxReceipt {
		return fmt.Errorf("reclaim receipt exceeds %d bytes", maxReceipt)
	}
	path := filepath.Join(dir, "reclaim.log")
	// A leading delimiter preserves this receipt even if a crash left the previous
	// append incomplete. Empty lines are ignored by the reader.
	line := append(append([]byte{'\n'}, b...), '\n')
	_, err = (&logfile.Writer{Path: path, Durable: true}).Write(line)
	return err
}

// Read is local and read-only. Missing logs mean empty history; corrupt records
// are skipped with a warning so a crash tail cannot hide earlier actions.
func Read(dir string, limit int, kind string) (History, error) {
	h := History{Entries: []Entry{}}
	if limit < 1 || limit > 1000 {
		return h, fmt.Errorf("history limit must be between 1 and 1000")
	}
	if kind != "" && kind != "process" && kind != "disk" {
		return h, fmt.Errorf("history kind must be process or disk")
	}
	for _, name := range []string{"reclaim.log.1", "reclaim.log", "cleanup.log.1", "cleanup.log"} {
		disk := name == "cleanup.log" || name == "cleanup.log.1"
		if kind == "process" && disk || kind == "disk" && !disk {
			continue
		}
		f, err := os.Open(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return h, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return h, err
		}
		start := max(int64(0), info.Size()-logfile.MaxSize)
		r := bufio.NewReader(io.NewSectionReader(f, start, logfile.MaxSize))
		if start > 0 {
			_, _ = r.ReadString('\n')
		}
		damaged := false
		for {
			line, readErr := r.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				var entry Entry
				if readErr == io.EOF || len(line) > 1<<20 || decode(line, disk, &entry) != nil {
					damaged = true
				} else {
					h.Entries = append(h.Entries, entry)
					// Bound retained memory as well as bytes read; the final sort merges logs.
					if len(h.Entries) > limit*2 {
						newest(h.Entries)
						h.Entries = h.Entries[:limit]
					}
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					f.Close()
					return h, readErr
				}
				break
			}
		}
		if err := f.Close(); err != nil {
			return h, err
		}
		if damaged {
			h.Warnings = append(h.Warnings, "Skipped incomplete or invalid records in "+name)
		}
	}
	newest(h.Entries)
	if len(h.Entries) > limit {
		h.Entries = h.Entries[:limit]
	}
	return h, nil
}

func newest(entries []Entry) {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.After(entries[j].At) })
}

func decode(line []byte, disk bool, entry *Entry) error {
	if !disk {
		if err := json.Unmarshal(line, entry); err != nil {
			return err
		}
		if entry.At.IsZero() || entry.Kind != "process" || entry.Status == "" {
			return fmt.Errorf("invalid process receipt")
		}
		return nil
	}
	// Decode only receipt fields; never expose cleanup command arguments or paths.
	var saved struct {
		At        time.Time `json:"occurred_at"`
		Candidate struct {
			ID string `json:"id"`
		} `json:"candidate"`
		Result struct {
			FreedBytes int64 `json:"freed_bytes"`
			DryRun     bool  `json:"dry_run"`
		} `json:"result"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(line, &saved); err != nil {
		return err
	}
	if saved.At.IsZero() || saved.Candidate.ID == "" {
		return fmt.Errorf("invalid disk receipt")
	}
	*entry = Entry{At: saved.At, Kind: "disk", ID: saved.Candidate.ID, Status: "completed", FreedBytes: saved.Result.FreedBytes, DryRun: saved.Result.DryRun}
	if saved.Error != "" {
		entry.Status = "failed"
		entry.ErrorCode = "cleanup_failed"
	}
	if entry.DryRun && saved.Error == "" {
		entry.Status = "dry_run"
	}
	return nil
}

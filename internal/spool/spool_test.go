package spool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestAppendDrainRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	dir := filepath.Join(t.TempDir(), "events")
	now := time.Date(2026, 10, 18, 23, 0, 0, 0, time.FixedZone("west", -7*3600))
	events := []model.HookEvent{{TS: now, Adapter: "claude", SessionID: "one", Event: "Stop"}, {TS: now.Add(-48 * time.Hour), Adapter: "codex", SessionID: "two", Event: "PermissionRequest"}}
	for _, ev := range events {
		if err := Append(dir, ev); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "20261019.jsonl")
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("bad json\nnull\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for path, mode := range map[string]os.FileMode{dir: 0700, file: 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: %v %v", path, info, err)
		}
	}
	got, offsets, err := Drain(dir, nil)
	if err != nil || len(got) != 2 || got[0].SessionID != "two" || got[1].SessionID != "one" {
		t.Fatal(got, err)
	}
	if len(offsets) != 2 || offsets["20261019.jsonl"].Pos == 0 {
		t.Fatal(offsets)
	}
	copyOffsets := make(map[string]model.Offset)
	for k, v := range offsets {
		copyOffsets[k] = v
	}
	more, next, err := Drain(dir, offsets)
	if err != nil || len(more) != 0 || !reflect.DeepEqual(next, offsets) {
		t.Fatal(more, next, err)
	}
	if !reflect.DeepEqual(offsets, copyOffsets) {
		t.Fatal("input offsets changed")
	}
	ev := events[0]
	ev.TS = ev.TS.Add(time.Second)
	if err := Append(dir, ev); err != nil {
		t.Fatal(err)
	}
	more, _, err = Drain(dir, offsets)
	if err != nil || len(more) != 1 || !more[0].TS.Equal(ev.TS) {
		t.Fatal(more, err)
	}
	missing, off, err := Drain(filepath.Join(t.TempDir(), "missing"), nil)
	if err != nil || len(missing) != 0 || len(off) != 0 {
		t.Fatal(missing, off, err)
	}
}

func TestAppendConcurrentLinesIntact(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := Append(dir, model.HookEvent{TS: now, Adapter: "claude", SessionID: fmt.Sprint(i), Event: "Stop", Message: strings.Repeat("\"世\n", 4000)}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	f, err := os.Open(filepath.Join(dir, now.Format("20060102")+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	seen := map[string]bool{}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line)+1 > 4096 {
			t.Fatalf("line %d bytes", len(line)+1)
		}
		var ev model.HookEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatal(err)
		}
		if seen[ev.SessionID] {
			t.Fatal("duplicate", ev.SessionID)
		}
		seen[ev.SessionID] = true
	}
	if err := scanner.Err(); err != nil || len(seen) != 50 {
		t.Fatal(len(seen), err)
	}
}

func TestDrainPartialAndRotatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "20261018.jsonl")
	ev := model.HookEvent{TS: time.Now().UTC(), Adapter: "claude", SessionID: "a", Event: "Stop"}
	b, _ := json.Marshal(ev)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	got, off, err := Drain(dir, nil)
	if err != nil || len(got) != 0 || off[filepath.Base(path)].Pos != 0 {
		t.Fatal(got, off, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\n")
	f.Close()
	got, off, err = Drain(dir, off)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	got, _, err = Drain(dir, off)
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
}

// AppendAll encodes every event before writing, so one unencodable event
// leaves none of the batch in the spool.
func TestAppendAllWritesNothingOnEncodeError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "events")
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	good := model.HookEvent{TS: now, Event: "summary", Summary: &model.Summary{Text: "fine"}}
	huge := model.HookEvent{TS: now, Event: "summary", Summary: &model.Summary{Text: strings.Repeat("x", 5000)}}
	if err := AppendAll(dir, []model.HookEvent{good, huge}); err == nil {
		t.Fatal("oversized event accepted")
	}
	events, _, err := Drain(dir, nil)
	if err != nil || len(events) != 0 {
		t.Fatalf("partial batch: %+v %v", events, err)
	}
	if err := AppendAll(dir, []model.HookEvent{good, good}); err != nil {
		t.Fatal(err)
	}
	if events, _, err = Drain(dir, nil); err != nil || len(events) != 2 {
		t.Fatalf("batch: %+v %v", events, err)
	}
}

func TestAppendCreatesParentsAndWakesExistingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "spool")
	ev := model.HookEvent{TS: time.Now(), Adapter: "claude", Event: "Stop"}
	for i := 0; i < 2; i++ {
		if i > 0 {
			old := time.Now().Add(-time.Hour)
			if err := os.Chtimes(dir, old, old); err != nil {
				t.Fatal(err)
			}
		}
		if err := AppendAll(dir, []model.HookEvent{ev, ev}); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil || time.Since(info.ModTime()) > time.Minute {
			t.Fatal("directory wake missing", info, err)
		}
	}
	got, _, err := Drain(dir, nil)
	if err != nil || len(got) != 4 {
		t.Fatal("lost batched appends", len(got), err)
	}
}

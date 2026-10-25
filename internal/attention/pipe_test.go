package attention

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestPipeNotifierRoundTrip(t *testing.T) {
	want := []Notification{
		{Kind: "waiting", Title: "Needs you", Body: "First line\nSecond line · approve?", Sound: "Glass", ItemID: "item-1", MachineID: "machine-1"},
		{Kind: "finished", Title: "Finished", Body: strings.Repeat("x", 128<<10), Sound: "Pop"},
		{},
	}
	var buf bytes.Buffer
	var notifier Notifier = &PipeNotifier{W: &buf}
	for _, n := range want {
		if err := notifier.Notify(context.Background(), n); err != nil {
			t.Fatal(err)
		}
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d", len(lines), len(want))
	}
	for i, line := range lines {
		var fields map[string]string
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatal(err)
		}
		n := want[i]
		if expected := map[string]string{"kind": n.Kind, "title": n.Title, "body": n.Body, "sound": n.Sound, "item_id": n.ItemID, "machine_id": n.MachineID}; !reflect.DeepEqual(fields, expected) {
			t.Fatalf("line %d has wrong JSON fields", i)
		}
	}
	var got []Notification
	if err := ReadNotifications(&buf, func(n Notification) { got = append(got, n) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("notifications changed during round trip")
	}
}

// Yield between bytes so concurrent writes would interleave without the notifier's lock.
type yieldingWriter struct{ bytes.Buffer }

func (w *yieldingWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		w.WriteByte(b)
		runtime.Gosched()
	}
	return len(p), nil
}

func TestPipeNotifierConcurrent(t *testing.T) {
	var w yieldingWriter
	notifier := &PipeNotifier{W: &w}
	const count = 50
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := notifier.Notify(context.Background(), Notification{ItemID: fmt.Sprint(i)}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(w.String(), "\n"), "\n")
	if len(lines) != count {
		t.Fatalf("got %d lines, want %d", len(lines), count)
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("interleaved JSON: %q", line)
		}
	}
	if err := ReadNotifications(&w, func(n Notification) {
		if seen[n.ItemID] {
			t.Errorf("duplicate item %q", n.ItemID)
		}
		seen[n.ItemID] = true
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != count {
		t.Fatalf("got %d unique items, want %d", len(seen), count)
	}
}

type failingNotificationIO struct{ err error }

func (f failingNotificationIO) Write([]byte) (int, error) { return 0, f.err }
func (f failingNotificationIO) Read([]byte) (int, error)  { return 0, f.err }

func TestPipeNotificationErrors(t *testing.T) {
	want := errors.New("pipe failed")
	n := &PipeNotifier{W: failingNotificationIO{want}}
	if err := n.Notify(context.Background(), Notification{}); !errors.Is(err, want) {
		t.Fatalf("write error = %v, want %v", err, want)
	}
	if err := ReadNotifications(failingNotificationIO{want}, func(Notification) {}); !errors.Is(err, want) {
		t.Fatalf("read error = %v, want %v", err, want)
	}
	for _, input := range []string{"", "{}\n", "{}"} {
		if err := ReadNotifications(strings.NewReader(input), func(Notification) {}); err != nil {
			t.Fatalf("valid input %q: %v", input, err)
		}
	}
}

func TestReadNotificationsSkipsMalformedLines(t *testing.T) {
	input := "invalid\n{\"item_id\":\"a\"}\n{\n\n{\"item_id\":\"b\"}\n{\"item_id\":"
	var got []string
	if err := ReadNotifications(strings.NewReader(input), func(n Notification) { got = append(got, n.ItemID) }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("delivered %v, want [a b]", got)
	}
}

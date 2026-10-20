package chat

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/store"
)

type fakeAgent struct {
	starts  chan Request
	release chan struct{}
	mu      sync.Mutex
	resumes []string
}

func (a *fakeAgent) Run(ctx context.Context, r Request, emit func(Delta) error) error {
	a.mu.Lock()
	a.resumes = append(a.resumes, r.Resume)
	a.mu.Unlock()
	a.starts <- r
	if err := emit(Delta{SessionID: "vendor-session"}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.release:
	}
	return emit(Delta{Text: "answer ghp_secret", Tool: "mcp__driga__get_summary"})
}
func fixture(t *testing.T, snippets bool) (*Broker, *fakeAgent, *store.Store) {
	t.Helper()
	st, err := store.Open("", store.Memory)
	if err != nil {
		t.Fatal(err)
	}
	a := &fakeAgent{starts: make(chan Request, 20), release: make(chan struct{}, 20)}
	b, err := New(context.Background(), st, a, Options{Snippets: snippets, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close(); st.Close() })
	return b, a, st
}
func settled(t *testing.T, b *Broker, id string) State {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s, e := b.Read(context.Background(), id, 0)
		if e != nil {
			t.Fatal(e)
		}
		if !s.Active && s.Queued == 0 {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("turn did not finish")
	return State{}
}
func TestQueueCancelResumeAndCapture(t *testing.T) {
	b, a, st := fixture(t, false)
	ctx := context.Background()
	c, e := b.Create(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = b.Send(ctx, c.ID, "first private question", Context{Page: "flow"}); e != nil {
		t.Fatal(e)
	}
	<-a.starts
	if e = b.Send(ctx, c.ID, "second private question", Context{}); e != nil {
		t.Fatal(e)
	}
	select {
	case <-a.starts:
		t.Fatal("concurrent turn")
	default:
	}
	a.release <- struct{}{}
	r := <-a.starts
	if r.Resume != "vendor-session" {
		t.Fatal(r)
	}
	if e = b.Stop(ctx, c.ID); e != nil {
		t.Fatal(e)
	}
	s := settled(t, b, c.ID)
	if s.Conversation.SessionID != "vendor-session" {
		t.Fatal(s)
	}
	rows, e := st.ChatMessages(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range rows {
		if strings.Contains(m.Text, "private") || strings.Contains(m.Text, "answer") {
			t.Fatal("titles captured body", m)
		}
	}
	b.Close()
	next, e := New(ctx, st, a, Options{DataDir: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	if e = next.Send(ctx, c.ID, "third", Context{}); e != nil {
		t.Fatal(e)
	}
	if r = <-a.starts; r.Resume != "vendor-session" {
		t.Fatal(r)
	}
	next.Stop(ctx, c.ID)
}
func TestSnippetsAndReplayBounds(t *testing.T) {
	b, a, st := fixture(t, true)
	ctx := context.Background()
	c, _ := b.Create(ctx)
	if e := b.Send(ctx, c.ID, strings.Repeat("x", 400)+" ghp_secret", Context{}); e != nil {
		t.Fatal(e)
	}
	<-a.starts
	a.release <- struct{}{}
	s := settled(t, b, c.ID)
	rows, e := st.ChatMessages(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range rows {
		if len([]rune(m.Text)) > 200 || strings.Contains(m.Text, "ghp_secret") {
			t.Fatal(m)
		}
	}
	replay, e := b.Read(ctx, c.ID, s.Cursor-1)
	if e != nil || len(replay.Events) != 1 || replay.Events[0].Seq != s.Cursor {
		t.Fatal(replay, e)
	}
	if e := b.Send(ctx, c.ID, strings.Repeat("a", MaxText+1), Context{}); e == nil {
		t.Fatal("unbounded input")
	}
}
func TestQueueBoundAndStopClearsQueue(t *testing.T) {
	b, a, _ := fixture(t, true)
	ctx := context.Background()
	c, _ := b.Create(ctx)
	b.Send(ctx, c.ID, "first", Context{})
	<-a.starts
	for i := 0; i < MaxQueue; i++ {
		if e := b.Send(ctx, c.ID, "queued", Context{}); e != nil {
			t.Fatal(e)
		}
	}
	if e := b.Send(ctx, c.ID, "overflow", Context{}); e == nil {
		t.Fatal("queue unbounded")
	}
	b.Stop(ctx, c.ID)
	s := settled(t, b, c.ID)
	if s.Queued != 0 {
		t.Fatal(s)
	}
}

func TestReplayExpiryAndMessageOrder(t *testing.T) {
	b, _, _ := fixture(t, true)
	ctx := context.Background()
	row, _ := b.Create(ctx)
	b.mu.Lock()
	c, err := b.load(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := store.ChatMessage{ID: "first", Role: "assistant", Text: "first"}
	second := store.ChatMessage{ID: "second", Role: "user", Text: "queued"}
	b.message(c, first)
	b.message(c, second)
	old := c.seq
	for i := 0; i < MaxEvents+5; i++ {
		first.Text = strings.Repeat("x", 4096)
		b.message(c, first)
	}
	if len(c.messages) != 2 || c.messages[0].ID != "first" {
		t.Fatal(c.messages)
	}
	b.mu.Unlock()
	state, err := b.Read(ctx, row.ID, old)
	if err != nil || !state.Reset {
		t.Fatal(state, err)
	}
}

func TestCaptureDowngradeAndInterruptedRecovery(t *testing.T) {
	b, a, st := fixture(t, true)
	ctx := context.Background()
	c, _ := b.Create(ctx)
	b.Send(ctx, c.ID, "private question", Context{})
	<-a.starts
	a.release <- struct{}{}
	settled(t, b, c.ID)
	b.Close()
	next, err := New(ctx, st, a, Options{Snippets: false, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	rows, err := st.ChatMessages(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Role != "tool" && row.Text != "" {
			t.Fatal("policy downgrade retained body", row)
		}
	}
}

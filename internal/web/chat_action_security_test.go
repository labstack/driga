package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
)

func TestChatActionPOSTs(t *testing.T) {
	s, b, id := chatFixture(t)
	for _, route := range []string{"mode", "decide"} {
		if w := request(s, "GET", "/chat/api/"+route); w.Code != 405 {
			t.Fatal(w.Code)
		}
	}
	if w := postChat(s, "/chat/api/mode", fmt.Sprintf(`{"id":%q,"mode":"full auto"}`, id)); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	state, err := b.Read(context.Background(), id, 0)
	if err != nil || state.Mode != chat.FullAuto {
		t.Fatal(state, err)
	}
	initial := waitChat(t, s, id, 0)
	if initial.Mode != chat.FullAuto || initial.Until.IsZero() {
		t.Fatal("web snapshot omitted trust mode or expiry", initial.State)
	}
	if w := postChat(s, "/chat/api/mode", fmt.Sprintf(`{"id":%q,"mode":"full auto"}`, id)); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	renewed := waitChat(t, s, id, initial.Cursor)
	if !renewed.Until.After(initial.Until) || len(renewed.Events) != 1 || renewed.Events[0].Mode != chat.FullAuto || !renewed.Events[0].Until.Equal(renewed.Until) {
		t.Fatal("web replay omitted renewed expiry", renewed.State)
	}
}

type approvalWebAgent struct{ requests chan chat.Request }

func (a approvalWebAgent) Run(ctx context.Context, r chat.Request, _ func(chat.Delta) error) error {
	a.requests <- r
	<-ctx.Done()
	return ctx.Err()
}

// Exercise the existing broker -> web snapshot/replay path, including the
// titles-only policy: live approval input is available but never persisted.
func TestWebApprovalInputAndActorRespectCapturePolicy(t *testing.T) {
	s, st, _, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	agent := approvalWebAgent{requests: make(chan chat.Request, 1)}
	b, err := chat.New(ctx, st, agent, chat.Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	s.opts.Chat = b
	row, err := b.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Send(ctx, row.ID, "private question", chat.Context{}); err != nil {
		t.Fatal(err)
	}
	var turn chat.Request
	select {
	case turn = <-agent.requests:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	input, _ := json.Marshal(map[string]string{"command": strings.Repeat("echo safe; ", 60) + "rm -rf ./scratch-test"})
	done := make(chan error, 1)
	go func() {
		_, err := b.Approve(ctx, chat.Approval{Conversation: row.ID, Token: turn.Token, Tool: "Bash", Input: input})
		done <- err
	}()
	var pending chatHTMLState
	for {
		pending = waitChat(t, s, row.ID, 0)
		if len(pending.Actions) == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	action := pending.Actions[0]
	if string(action.Input) != string(input) || action.Directory != turn.Directory || action.Tool != "Bash" || action.Status != "pending" {
		t.Fatal("incomplete approval payload", action)
	}
	if w := postChat(s, "/chat/api/decide", fmt.Sprintf(`{"id":%q,"action":%q,"allow":false}`, row.ID, action.ID)); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	replay := waitChat(t, s, row.ID, pending.Cursor)
	if len(replay.Events) != 1 || replay.Events[0].Action == nil {
		t.Fatal("missing decision event", replay.State)
	}
	decided := replay.Events[0].Action
	if decided.DecidedBy != "you" || decided.Status != "denied" || string(decided.Input) != string(input) || decided.Directory != turn.Directory {
		t.Fatal("incomplete decision payload", decided)
	}
	b.Close()
	next, err := chat.New(ctx, st, nil, chat.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	s.opts.Chat = next
	retained := waitChat(t, s, row.ID, 0)
	if len(retained.Actions) != 1 || retained.Actions[0].DecidedBy != "you" || string(retained.Actions[0].Input) != "{}" || retained.Actions[0].Directory != "" {
		t.Fatal("capture policy lost metadata or retained private input", retained.State)
	}
	for _, message := range retained.Messages {
		if message.Text != "" {
			t.Fatal("titles-only capture retained private text", message)
		}
	}
}

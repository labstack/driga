package web

import (
	"strings"
	"testing"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/store"
)

func TestChatServerHTMLReplay(t *testing.T) {
	initial := store.ChatMessage{ID: "a", Role: "assistant", Text: "", Status: "streaming"}
	final := initial
	final.Text = "**Hello**"
	final.Status = "complete"
	user := store.ChatMessage{ID: "u", Role: "user", Text: "**user**"}
	state := chat.State{Messages: []store.ChatMessage{user, final}, Events: []chat.Event{
		{Seq: 1, Kind: "message", Message: &initial},
		{Seq: 2, Kind: "delta", MessageID: "a", Text: "**Hel"},
		{Seq: 3, Kind: "delta", MessageID: "a", Text: "lo**"},
		{Seq: 4, Kind: "message", Message: &final},
	}}
	if got := renderChatMessage(store.ChatMessage{Role: "assistant", Status: "streaming", Text: "**streaming**"}); got.HTML != "" {
		t.Fatal("streaming reset rendered Markdown", got.HTML)
	}
	got := renderChatState(state)
	if got.Messages[0].HTML != "" || got.Messages[1].HTML != chat.MarkdownHTML(final.Text) {
		t.Fatal(got.Messages)
	}
	if got.Events[1].HTML != "" || got.Events[2].HTML != "" || got.Events[3].Message.HTML != chat.MarkdownHTML(final.Text) {
		t.Fatal(got.Events)
	}
	if state.Events[0].Message.Text != "" {
		t.Fatal("mutated broker state")
	}
	// A reconnect can start in the middle of a stream, without an initial event.
	state.Events = state.Events[2:3]
	if got := renderChatState(state).Events[0].HTML; got != "" {
		t.Fatal(got)
	}
	state.Messages[1].Text = strings.Repeat("a", chat.MaxText+1024)
	got = renderChatState(state)
	if len(got.Messages[1].HTML) > chat.MaxText+8 {
		t.Fatal("unbounded rendering")
	}
}

package chat

import (
	"context"
	"testing"
)

func TestSocketApprovalCapabilities(t *testing.T) {
	b, a, _ := fixture(t, true)
	dir := t.TempDir()
	closeSocket, err := Listen(context.Background(), dir, b)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSocket()
	client, err := Dial(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	in := activeApproval(t, b, a, Context{})
	if err := client.SetMode(context.Background(), in.Conversation, FullAuto); err != nil {
		t.Fatal(err)
	}
	p, err := Bridge(context.Background(), SocketPath(dir), in)
	if err != nil || p.Behavior != "allow" {
		t.Fatal(p, err)
	}
	child, err := dialPath(context.Background(), SocketPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	child.token = in.Token
	if err := child.SetMode(context.Background(), in.Conversation, Confirm); err == nil {
		t.Fatal("turn token changed trust mode")
	}
	if err := child.Decide(context.Background(), in.Conversation, "anything", true); err == nil {
		t.Fatal("turn token decided a card")
	}
	if err := child.call(context.Background(), "POST", "/approve", map[string]any{"conversation": in.Conversation, "token": in.Token, "mode": "confirm", "allow": true}, nil); err == nil {
		t.Fatal("approve accepted client fields")
	}
	s, _ := b.Read(context.Background(), in.Conversation, 0)
	if s.Mode != FullAuto {
		t.Fatal(s.Mode)
	}
}

package chat

import (
	"context"
	"strings"
	"testing"
)

func TestReadOnlyNoticePerConversation(t *testing.T) {
	cli, _ := fakeCLI(t, flags, `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}]}'
echo '{"type":"result","result":"ok"}'`)
	b, _, _ := fixture(t, true)
	b.agent = cli
	for range 2 {
		row, err := b.Create(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for turn := range 3 {
			if turn == 2 {
				b.mu.Lock()
				b.dispose(b.conversations[row.ID])
				delete(b.conversations, row.ID)
				b.mu.Unlock()
			}
			if err := b.Send(context.Background(), row.ID, "question", Context{}); err != nil {
				t.Fatal(err)
			}
			s := settled(t, b, row.ID)
			count := 0
			for _, m := range s.Messages {
				if m.Role == "system" && (m.Status == "notice" || strings.HasPrefix(m.Text, "Chat stays read-only:")) {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("conversation %s turn %d: %d notices", row.ID, turn, count)
			}
		}
	}
}

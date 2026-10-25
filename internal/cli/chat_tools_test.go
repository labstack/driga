package cli

import (
	"context"
	"slices"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/labstack/driga/internal/chat"
)

// Chat refuses to start when its MCP server offers a tool it does not
// expect (chat.Claude), so the server must offer exactly what each Chat mode
// accepts: read-only Chat only the read tools, Chat with actions every
// dashboard tool, delete included, which waits on approval.
// An outside MCP client keeps every dashboard tool.
func TestChatServerOffersOnlyWhatChatAccepts(t *testing.T) {
	list := func(forChat, actions bool) []string {
		ctx := context.Background()
		a, b := sdk.NewInMemoryTransports()
		ss, err := newMCPServer(&Globals{}, forChat, actions, "titles").Connect(ctx, a, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ss.Close()
		cs, err := sdk.NewClient(&sdk.Implementation{Name: "test"}, nil).Connect(ctx, b, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer cs.Close()
		listing, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range listing.Tools {
			// A tool only the rendered app calls is hidden from the model.
			if ui, ok := tool.Meta["ui"].(map[string]any); ok {
				if v, ok := ui["visibility"].([]any); ok && !slices.Contains(v, any("model")) {
					continue
				}
			}
			names = append(names, tool.Name)
		}
		return names
	}
	for _, name := range list(true, false) {
		if !slices.Contains(chat.ReadTools, name) {
			t.Errorf("read-only Chat is offered %q, which it refuses", name)
		}
	}
	withActions := list(true, true)
	if !slices.Contains(withActions, "delete_dashboard") || !slices.Contains(withActions, "edit_dashboard") {
		t.Errorf("Chat with actions is offered %v", withActions)
	}
	if outside := list(false, false); !slices.Contains(outside, "delete_dashboard") || !slices.Contains(outside, "create_dashboard") {
		t.Errorf("an outside client is offered %v", outside)
	}
}

package chat

import (
	"context"
	"encoding/json"
	"github.com/labstack/driga/internal/command"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCommandAuthorizationBinding(t *testing.T) {
	for _, field := range []string{"argv", "script", "cwd", "timeout", "network", "project", "backend", "targets", "replay"} {
		t.Run(field, func(t *testing.T) {
			b, in, project := commandTurn(t)
			sideEffects := field == "replay" && command.ProbeSandbox(context.Background()).Available
			if sideEffects {
				in.Input = json.RawMessage(`{"script":"echo run >> invocations"}`)
			}
			ctx := context.Background()
			p, err := b.prepareCommand(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := b.approveCommand(ctx, in, p); done <- err }()
			card := waitAction(t, b, in.Conversation)
			if err = b.Decide(ctx, in.Conversation, card.ID, true); err != nil {
				t.Fatal(err)
			}
			if err = <-done; err != nil {
				t.Fatal(err)
			}
			digest := command.Digest(p)
			b.mu.Lock()
			a := b.conversations[in.Conversation].actions[0]
			switch field {
			case "argv":
				a.prepared.Parsed.Request.Argv[0] = "sh"
			case "script":
				a.prepared.Parsed.Request.Script = "true"
			case "cwd":
				a.prepared.Parsed.Request.Cwd = "/"
			case "timeout":
				a.prepared.Parsed.Request.TimeoutSeconds = 600
			case "network":
				a.prepared.Parsed.Request.Network = true
			case "project":
				a.prepared.Scope.Project = "/"
			case "backend":
				a.prepared.Sandbox.Backend = "changed"
			case "targets":
				a.prepared.ProcessSnapshot = &command.ProcessSnapshot{Force: true}
			}
			b.mu.Unlock()
			if field != "replay" {
				if _, err = b.executeCommand(ctx, in, card.ID, digest); err == nil {
					t.Fatal("changed authority executed")
				}
				return
			}
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for range 2 {
				wg.Go(func() { _, err := b.executeCommand(ctx, in, card.ID, digest); results <- err })
			}
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				}
			}
			if success != 1 {
				t.Fatal("concurrent executions", success)
			}
			if _, err = b.executeCommand(ctx, in, card.ID, digest); err == nil {
				t.Fatal("replay executed")
			}
			if sideEffects {
				data, err := os.ReadFile(filepath.Join(project, "invocations"))
				if err != nil || strings.Count(string(data), "run") != 1 {
					t.Fatal("command side effects repeated", string(data), err)
				}
			}
			if _, err = b.Claim(in); err == nil {
				t.Fatal("child consumed command authority")
			}
		})
	}
}

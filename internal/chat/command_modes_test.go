package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/driga/internal/command"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutsideReadsAlwaysRequireCard(t *testing.T) {
	for _, mode := range []Mode{Confirm, Auto, FullAuto} {
		for _, operand := range []string{"outside", "symlink", "tilde"} {
			t.Run(string(mode)+"/"+operand, func(t *testing.T) {
				b, in, project := commandTurn(t)
				outside := filepath.Join(t.TempDir(), "notes.txt")
				if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
					t.Fatal(err)
				}
				path := outside
				if operand == "symlink" {
					path = filepath.Join(project, "escape")
					if err := os.Symlink(outside, path); err != nil {
						t.Fatal(err)
					}
				}
				req := command.Request{Argv: []string{"cat", path}}
				if operand == "tilde" {
					req = command.Request{Script: "cat ~/notes.txt"}
				}
				in.Input, _ = json.Marshal(req)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				p, err := b.prepareCommand(ctx, in)
				if err != nil {
					t.Fatal(err)
				}
				p.Sandbox.Available = true // Approval seam only; no execution.
				if commandAutomatic(mode, p) {
					t.Fatalf("%s auto-approved %s", mode, operand)
				}
				if err := b.SetMode(ctx, in.Conversation, mode); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { _, err := b.approveCommand(ctx, in, p); done <- err }()
				card := waitAction(t, b, in.Conversation)
				if card.Status != "pending" || card.GrantEligible {
					t.Fatal("read needs an individual card", card)
				}
				if err := b.Decide(ctx, in.Conversation, card.ID, false); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err == nil {
					t.Fatal("denial ignored")
				}
			})
		}
	}
}

func TestCommandRunnersCardedInFullAuto(t *testing.T) {
	for _, argv := range [][]string{
		{"flock", ".lock", "/bin/rm", "-rf", "victim"},
		{"unbuffer", "rm", "victim"},
		{"script", "-c", "rm victim", "typescript"},
		{"watch", "rm victim"},
		{"parallel", "rm", "{}", ":::", "victim"},
	} {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			b, in, _ := commandTurn(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			in.Input, _ = json.Marshal(command.Request{Argv: argv})
			p, err := b.prepareCommand(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			// Exercise approval with a sandbox even on hosts without a backend.
			// No command is executed by this test.
			p.Sandbox.Available = true
			if commandAutomatic(FullAuto, p) {
				t.Fatalf("Full auto would execute %v without a card (class %s)", argv, p.Policy.Class)
			}
			if err := b.SetMode(ctx, in.Conversation, FullAuto); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, err := b.approveCommand(ctx, in, p); done <- err }()
			card := waitAction(t, b, in.Conversation)
			if card.Status != "pending" || card.Command == nil || card.Command.Class != command.Destructive || card.GrantEligible {
				t.Fatal("wrapper did not require approval", card)
			}
			if err := b.Decide(ctx, in.Conversation, card.ID, false); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err == nil {
				t.Fatal("wrapper denial ignored")
			}
		})
	}
}

func TestCommandModeMatrix(t *testing.T) {
	for _, mode := range []Mode{Confirm, Auto, FullAuto} {
		for _, class := range []command.Class{command.Read, command.Process, command.Other, command.Destructive, command.Never} {
			for _, available := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", mode, class, available), func(t *testing.T) {
					b, in, _ := commandTurn(t)
					ctx := context.Background()
					p, err := b.prepareCommand(ctx, in)
					if err != nil {
						t.Fatal(err)
					}
					p.Policy.Class = class
					p.Sandbox.Available = available
					p.Sandbox.Reason = "test sandbox unavailable"
					if err = b.SetMode(ctx, in.Conversation, mode); err != nil {
						t.Fatal(err)
					}
					done := make(chan error, 1)
					go func() { _, err := b.approveCommand(ctx, in, p); done <- err }()
					if class == command.Never {
						select {
						case err := <-done:
							if err == nil {
								t.Fatal("Never allowed")
							}
						case <-time.After(time.Second):
							t.Fatal("Never carded")
						}
						return
					}
					unavailable := !available && (class == command.Other || class == command.Destructive)
					automatic := (mode == Auto || mode == FullAuto) && class == command.Read || mode == FullAuto && class == command.Other && available
					if automatic || unavailable {
						select {
						case err := <-done:
							if (err == nil) != automatic {
								t.Fatal(err)
							}
						case <-time.After(time.Second):
							t.Fatal("unexpected pending card")
						}
						return
					}
					card := waitAction(t, b, in.Conversation)
					if card.Command.Class != class {
						t.Fatal(card)
					}
					if err = b.Decide(ctx, in.Conversation, card.ID, false); err != nil {
						t.Fatal(err)
					}
					if err = <-done; err == nil {
						t.Fatal("denial ignored")
					}
				})
			}
		}
	}
	if FullAutoTTL != 30*time.Minute {
		t.Fatal("Full auto window changed")
	}
}
func TestDelegatedAllowAlwaysNeedsPerson(t *testing.T) {
	for _, mode := range []Mode{Confirm, Auto, FullAuto} {
		for _, change := range []string{"unchanged", "input", "tool", "session", "cwd", "retired", "reused"} {
			t.Run(string(mode)+"/"+change, func(t *testing.T) {
				b, in, _ := commandTurn(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				prompt := testPrompt("delegated")
				prompt.Input = json.RawMessage(`{"command":"echo example","nested":{"exact":"keep all input"}}`)
				go b.WaitPrompt(ctx, prompt)
				waitPrompt(t, b, prompt.ID)
				in.Tool = "mcp__driga__answer_prompt"
				in.Input = json.RawMessage(`{"id":"delegated","allow":true}`)
				if err := b.SetMode(ctx, in.Conversation, mode); err != nil {
					t.Fatal(err)
				}
				done := make(chan Permission, 1)
				go func() { p, _ := b.Approve(ctx, in); done <- p }()
				card := waitAction(t, b, in.Conversation)
				if card.Delegated == nil || string(card.Delegated.Input) != string(prompt.Input) {
					t.Fatal("exact input missing", card)
				}
				card.Delegated.Input[0] = '[' // Client copies must never alter authority.
				if err := b.Decide(ctx, in.Conversation, card.ID, true); err != nil {
					t.Fatal(err)
				}
				if p := <-done; p.Behavior != "allow" {
					t.Fatal(p)
				}
				id, err := b.Claim(in)
				if err != nil {
					t.Fatal(err)
				}
				b.mu.Lock()
				live := b.prompts[prompt.ID]
				switch change {
				case "input":
					live.Input = json.RawMessage(`{"command":"changed"}`)
				case "tool":
					live.Tool = "Write"
				case "session":
					live.Session = "different"
				case "cwd":
					live.Cwd = "/elsewhere"
				case "retired":
					delete(b.prompts, prompt.ID)
				case "reused":
					cp := *live
					b.prompts[prompt.ID] = &cp
				}
				b.mu.Unlock()
				err = b.AnswerApprovedPrompt(in, id)
				if (err == nil) != (change == "unchanged") {
					t.Fatal("changed prompt answered", err)
				}
				if err = b.AnswerApprovedPrompt(in, id); err == nil {
					t.Fatal("delegated answer replayed")
				}
			})
		}
	}
}

func TestCommandLegacyActionsAlwaysNeedPerson(t *testing.T) {
	for _, mode := range []Mode{Confirm, Auto, FullAuto} {
		for _, tool := range []string{"mcp__driga__kill", "mcp__driga__cleanup", "Unknown"} {
			t.Run(string(mode)+"/"+tool, func(t *testing.T) {
				b, in, _ := commandTurn(t)
				ctx := context.Background()
				in.Tool = tool
				in.Input = json.RawMessage(`{"id":"missing"}`)
				if err := b.SetMode(ctx, in.Conversation, mode); err != nil {
					t.Fatal(err)
				}
				done := make(chan Permission, 1)
				go func() { p, _ := b.Approve(ctx, in); done <- p }()
				card := waitAction(t, b, in.Conversation)
				if err := b.Decide(ctx, in.Conversation, card.ID, false); err != nil {
					t.Fatal(err)
				}
				if p := <-done; p.Behavior != "deny" {
					t.Fatal(p)
				}
			})
		}
	}
	b, in, _ := commandTurn(t)
	in.Tool = "mcp__driga__kill"
	in.Input = json.RawMessage(`{"id":"missing","force":true}`)
	if err := b.SetMode(context.Background(), in.Conversation, FullAuto); err != nil {
		t.Fatal(err)
	}
	p, err := b.Approve(context.Background(), in)
	if err != nil || p.Behavior != "deny" {
		t.Fatal("legacy force escaped second card", p, err)
	}
}

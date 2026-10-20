package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/view"
)

type AskCmd struct {
	Question []string `arg:"" required:"" help:"One-shot Chat question using Claude and driga's read-only tools."`
}

func (a AskCmd) Run(g *Globals) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg, err := config.Load(config.Path())
	if err != nil {
		return err
	}
	client, ephemeral, closeChat, err := chat.Open(ctx, config.DataDir(), cfg.CaptureSnippets())
	if err != nil {
		return err
	}
	defer closeChat()
	if ephemeral && g.Stderr != nil {
		fmt.Fprintln(g.Stderr, "Chat runs in-process; conversations end with it.")
	}
	conversation, err := client.Create(ctx)
	if err != nil {
		return err
	}
	if err = client.Send(ctx, conversation.ID, strings.Join(a.Question, " "), chat.Context{Page: "ask", Project: g.Project, Machine: g.Machine}); err != nil {
		return err
	}
	var result json.RawMessage
	printed := ""
	for {
		state, err := client.Read(ctx, conversation.ID, 0)
		if err != nil {
			return err
		}
		for _, m := range state.Messages {
			if m.Role != "assistant" {
				continue
			}
			text := m.Text
			if m.Status == "streaming" {
				text = chat.StreamSafePrefix(text)
			}
			if plain := chat.PlainCitations(text); !g.JSON && strings.HasPrefix(plain, printed) {
				if _, err := io.WriteString(g.Stdout, strings.TrimPrefix(plain, printed)); err != nil {
					_ = client.Stop(ctx, conversation.ID)
					return err
				}
				printed = plain
			}
			if m.Status == "error" {
				return fmt.Errorf("%s", strings.TrimSpace(m.Text))
			}
		}
		result = state.Result
		if !state.Active && state.Queued == 0 {
			break
		}
		select {
		case <-ctx.Done():
			_ = client.Stop(context.Background(), conversation.ID)
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	if g.JSON {
		r, mode, err := decideMode(ctx, g)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		envelope := view.Envelope{Schema: 2, ObservedAt: now, Mode: mode, Freshness: map[string]view.Fresh{}, CoverageGaps: []model.Gap{}, Data: result}
		if r != nil {
			defer r.Close()
			envelope, err = view.NewEnvelope(r, mode, now, collector.Cadence(0), result)
			if err != nil {
				return err
			}
		}
		return json.NewEncoder(g.Stdout).Encode(envelope)
	}
	_, err = fmt.Fprintln(g.Stdout)
	return err
}

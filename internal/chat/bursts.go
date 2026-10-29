package chat

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"
)

const PromptBurstWindow = 3 * time.Second

// A member's input and hash are frozen at registration, not at decision time.
type PromptBurstMember struct {
	ID        string `json:"id"`
	InputHash string `json:"input_hash"`
	Prompt    Prompt `json:"prompt"`
}
type PromptBurst struct {
	ID       string              `json:"id"`
	Session  string              `json:"session"`
	Cwd      string              `json:"cwd"`
	OpenedAt time.Time           `json:"opened_at"`
	Members  []PromptBurstMember `json:"members"`
}
type pendingBurst struct {
	PromptBurst
	epoch string
	live  map[string]*pendingPrompt
}
type BurstClient interface {
	PromptBursts(context.Context) ([]PromptBurst, error)
	DecideBurst(context.Context, string, []string, bool) error
}

func (b *Broker) pruneBursts() {
	for id, g := range b.bursts {
		live := false
		for member, p := range g.live {
			if b.prompts[member] == p && p.wait.Err() == nil {
				live = true
			} else {
				delete(g.live, member)
			}
		}
		if !live {
			delete(b.bursts, id)
			continue
		}
		// Keep only hashes/IDs for stale decisions; retired inputs must not become
		// an unbounded second prompt-history buffer.
		for i := range g.Members {
			if g.live[g.Members[i].ID] == nil {
				g.Members[i].Prompt = Prompt{}
			}
		}
	}
}
func (b *Broker) groupPrompt(p *pendingPrompt) {
	b.pruneBursts()
	if b.bursts == nil {
		b.bursts = map[string]*pendingBurst{}
	}
	var group *pendingBurst
	for _, g := range b.bursts {
		if g.epoch == p.sessionEpoch && g.Session == p.Session && g.Cwd == p.Cwd && !p.CreatedAt.Before(g.OpenedAt) && p.CreatedAt.Before(g.OpenedAt.Add(PromptBurstWindow)) {
			group = g
			break
		}
	}
	if group == nil {
		group = &pendingBurst{PromptBurst: PromptBurst{ID: uuid.NewString(), Session: p.Session, Cwd: p.Cwd, OpenedAt: p.CreatedAt}, epoch: p.sessionEpoch, live: map[string]*pendingPrompt{}}
		b.bursts[group.ID] = group
	}
	hash := promptDigest(p.Prompt)
	group.Members = append(group.Members, PromptBurstMember{ID: p.ID, InputHash: hex.EncodeToString(hash[:]), Prompt: copyPrompt(p.Prompt)})
	group.live[p.ID] = p
}
func (b *Broker) PromptBursts(ctx context.Context) ([]PromptBurst, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneBursts()
	out := []PromptBurst{}
	for _, g := range b.bursts {
		cp := g.PromptBurst
		cp.Members = []PromptBurstMember{}
		for _, m := range g.Members {
			p := g.live[m.ID]
			if p == nil || b.prompts[m.ID] != p || p.wait.Err() != nil {
				continue
			}
			m.Prompt = copyPrompt(m.Prompt)
			cp.Members = append(cp.Members, m)
		}
		if len(cp.Members) > 0 {
			out = append(out, cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OpenedAt.Equal(out[j].OpenedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].OpenedAt.Before(out[j].OpenedAt)
	})
	return out, nil
}
func (b *Broker) DecideBurst(ctx context.Context, id string, members []string, allow bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	g := b.bursts[id]
	if b.closed || ctx.Err() != nil || g == nil || len(members) == 0 || len(members) > MaxMessages {
		return errors.New("burst no longer waiting")
	}
	wanted := map[string]PromptBurstMember{}
	for _, m := range g.Members {
		wanted[m.ID] = m
	}
	seen := map[string]bool{}
	// Reject malformed or cross-group membership before answering any item.
	for _, member := range members {
		if _, ok := wanted[member]; !ok || seen[member] {
			return errors.New("invalid burst membership")
		}
		seen[member] = true
	}
	var stale []string
	for _, member := range members {
		m := wanted[member]
		p := b.prompts[member]
		if p == nil || p != g.live[member] || p.wait.Err() != nil || p.sessionEpoch != g.epoch {
			stale = append(stale, member)
			continue
		}
		hash := promptDigest(p.Prompt)
		if hex.EncodeToString(hash[:]) != m.InputHash {
			stale = append(stale, member)
			continue
		}
		if err := b.answerPrompt(member, allow); err != nil {
			stale = append(stale, member)
		}
	}
	b.pruneBursts()
	if len(stale) > 0 {
		return fmt.Errorf("stale burst members: %s", strings.Join(stale, ", "))
	}
	return nil
}
func (c *Remote) PromptBursts(ctx context.Context) ([]PromptBurst, error) {
	var out []PromptBurst
	err := c.call(ctx, "GET", "/prompt-bursts", nil, &out)
	return out, err
}
func (c *Remote) DecideBurst(ctx context.Context, id string, members []string, allow bool) error {
	return c.callOnce(ctx, "POST", "/burst-decide", map[string]any{"id": id, "members": members, "allow": allow}, nil, false)
}

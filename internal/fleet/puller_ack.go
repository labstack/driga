package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"time"
)

// SetPendingAcks supplies the writer's committed, durable pending requests.
// Only the authoritative inbox feed can clear a pending request in the store.
func (p *Puller) SetPendingAcks(ids []string) {
	next := make(map[string]bool, len(ids))
	for _, id := range ids {
		next[id] = true
	}
	p.ackMu.Lock()
	changed := !maps.Equal(p.acks, next)
	p.acks = next
	p.ackMu.Unlock()
	if changed {
		p.notifyAcks()
	}
}

func (p *Puller) notifyAcks() {
	select {
	case p.ackChanged <- struct{}{}:
	default:
	}
}

func (p *Puller) runAcks(ctx context.Context) {
	if !p.peer.Ack {
		return
	}
	sent := make(map[string]bool)
	var epoch uint64
	delay := 2 * time.Second
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.ackChanged:
		case <-timer.C:
		}
		if current := p.ackEpoch.Load(); current != epoch {
			clear(sent) // A reconnect retries requests whose receipt was lost.
			epoch = current
		}
		p.ackMu.Lock()
		pending := make(map[string]bool, len(p.acks))
		for id := range p.acks {
			pending[id] = true
		}
		p.ackMu.Unlock()
		for id := range sent {
			if !pending[id] {
				delete(sent, id)
			}
		}
		failed := false
		for id := range pending {
			if sent[id] {
				continue
			}
			if err := p.postAck(ctx, id); err != nil {
				failed = true
				break
			}
			sent[id] = true
		}
		if failed {
			timer.Reset(delay)
			delay = min(60*time.Second, delay*2)
		} else {
			delay = 2 * time.Second
			timer.Stop()
		}
	}
}

func (p *Puller) postAck(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body, err := json.Marshal(struct {
		ItemID string `json:"item_id"`
	}{id})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+p.peer.Addr+"/v1/ack", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 || resp.TLS.PeerCertificates[0].Subject.CommonName != p.peer.MachineID {
		return fmt.Errorf("peer machine_id mismatch; run driga fleet rekey on the peer (pairings must be redone)")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("fleet ack: %s", resp.Status)
	}
	return nil
}

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/labstack/driga/internal/jsonx"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
)

// FleetEnrollCmd opens one bounded pairing window on the running collector.
type FleetEnrollCmd struct {
	Ack    bool          `help:"Allow this viewer to acknowledge inbox items."`
	Allow  string        `help:"Pre-authorize exactly this viewer device ID (headless)."`
	Window time.Duration `help:"Enrollment duration (default fleet.enroll_window, at most 10m)."`
}

// Run asks for confirmation locally or returns immediately for a pinned viewer.
func (c FleetEnrollCmd) Run(g *Globals) error {
	overrides := map[string]any{}
	if c.Window != 0 {
		overrides["fleet.enroll_window"] = c.Window.String()
	}
	cfg, err := config.LoadWithOverrides(config.Path(), overrides)
	if err != nil {
		return err
	}
	window := cfg.Fleet.EnrollWindow
	ctx, stop := pairCommandContext()
	defer stop()
	status, err := fleet.Control(ctx, config.DataDir(), fleet.ControlRequest{Op: "status"})
	if err != nil {
		return fmt.Errorf("fleet collector unavailable: %w", err)
	}
	if _, err := fmt.Fprintf(g.Stdout, "peer id %s · listen %s · enrollment window %s\n", status.Status.DeviceID, status.Status.Listen, window); err != nil {
		return err
	}
	viewer, err := fleet.Enroll(ctx, config.DataDir(), fleet.EnrollOpts{Window: window, Ack: c.Ack, AllowID: c.Allow}, pairPrompt(g, "Does the viewer show the same code?"))
	if err != nil {
		return err
	}
	if c.Allow != "" {
		_, err = fmt.Fprintln(g.Stdout, "enrollment open for", c.Allow)
		return err
	}
	_, err = fmt.Fprintf(g.Stdout, "authorized viewer %s (%s) · read ✓ · ack %t · enrollment closed\n", viewer.Name, viewer.DeviceID, c.Ack)
	return err
}

// FleetAddCmd pairs this viewer to a serving machine.
type FleetAddCmd struct {
	Addr string `arg:"" name:"host" help:"Peer host[:port] (default port 7411)."`
	Name string `help:"Local name for the peer."`
	ID   string `help:"Require this peer device ID (headless)."`
}

// Run compares the code through an injected prompt and persists the peer.
func (c FleetAddCmd) Run(g *Globals) error {
	ctx, stop := pairCommandContext()
	defer stop()
	peer, err := fleet.Add(ctx, c.Addr, fleet.AddOpts{DataDir: config.DataDir(), Name: c.Name, ID: c.ID}, pairPrompt(g, "Does the peer show the same code?"))
	if err != nil {
		return err
	}
	if g.JSON {
		return json.NewEncoder(g.Stdout).Encode(peer)
	}
	ack := "✗"
	if peer.Ack {
		ack = "✓"
	}
	_, err = fmt.Fprintf(g.Stdout, "paired with %s (machine %s) · read ✓ · ack %s\n", peer.Name, peer.MachineID, ack)
	return err
}

func pairPrompt(g *Globals, question string) func(string) bool {
	reader := bufio.NewReader(g.Stdin)
	return func(code string) bool {
		if _, err := fmt.Fprintf(g.Stderr, "confirmation code  %s\n%s [y/N] ", code, question); err != nil {
			return false
		}
		line, err := reader.ReadString('\n')
		return err == nil && strings.EqualFold(strings.TrimSpace(line), "y")
	}
}
func pairCommandContext() (context.Context, context.CancelFunc) {
	return notifyContext(context.Background())
}

// FleetRevokeCmd immediately revokes a viewer by name or device ID.
type FleetRevokeCmd struct {
	Viewer string `arg:"" help:"Viewer name or device ID."`
}

// Run updates trust and closes the viewer's live connections.
func (c FleetRevokeCmd) Run(g *Globals) error {
	v, err := fleet.RevokeViewer(context.Background(), config.DataDir(), c.Viewer)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(g.Stdout, "revoked %s (%s)\n", v.Name, v.DeviceID)
	return err
}

// FleetRemoveCmd removes a peer locally, optionally deleting its cached rows.
type FleetRemoveCmd struct {
	Name  string `arg:"" help:"Peer name, device ID or machine ID (removed peers with --purge)."`
	Purge bool   `help:"Delete this peer's cached rows."`
}

// Run removes the peer; retained rows continue to age out by retention.
func (c FleetRemoveCmd) Run(g *Globals) error {
	p, err := fleet.RemovePeer(context.Background(), config.DataDir(), c.Name, c.Purge)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(g.Stdout, "removed peer", p.Name)
	return err
}

// FleetRekeyCmd replaces this device's certificate and invalidates all pairings.
type FleetRekeyCmd struct{}

// Run performs identity maintenance under the collector's exclusive lock.
func (FleetRekeyCmd) Run(g *Globals) error {
	if err := fleet.Rekey(context.Background(), config.DataDir()); err != nil {
		return err
	}
	_, err := fmt.Fprintln(g.Stdout, "identity regenerated; all pairings are void; viewers must re-pair")
	return err
}

// MachineResetIDCmd repairs a cloned machine's row identity and certificate.
type MachineResetIDCmd struct{}

// Run re-stamps local rows and invalidates old trust.
func (MachineResetIDCmd) Run(g *Globals) error {
	if err := fleet.ResetID(context.Background(), config.DataDir()); err != nil {
		return err
	}
	_, err := fmt.Fprintln(g.Stdout, "machine ID and identity regenerated; viewers must re-pair")
	return err
}

// FleetPeersCmd lists locally trusted peers and any recorded trust failure.
type FleetPeersCmd struct{}

// Run reads peers without contacting another machine.
func (FleetPeersCmd) Run(g *Globals) error {
	peers, err := fleet.LoadPeers(filepath.Join(config.DataDir(), "fleet"))
	if err != nil {
		return err
	}
	rows := make([]peerStatus, 0, len(peers))
	machines := map[string]model.Machine{}
	st, e := store.Open(filepath.Join(config.DataDir(), "driga.db"), store.ReadOnly)
	if e == nil {
		ms, readErr := st.Machines()
		closeErr := st.Close()
		if e := errors.Join(readErr, closeErr); e != nil {
			return e
		}
		for _, m := range ms {
			machines[m.MachineID] = m
		}
	} else if _, statErr := os.Stat(filepath.Join(config.DataDir(), "driga.db")); !os.IsNotExist(statErr) {
		return e
	}
	now := time.Now()
	for _, p := range peers {
		m := machines[p.MachineID]
		m.MachineID = p.MachineID
		state := view.MachineState(m, "", now)
		if p.State == "untrusted" {
			state = "untrusted"
		}
		seen := m.LastContactAt
		if seen.IsZero() {
			seen = m.LastSeen
		}
		p.State = state
		rows = append(rows, peerStatus{Peer: p, LastSeen: seen, LastError: m.LastError, FeedEpoch: m.FeedEpoch, FeedSeq: m.FeedSeq})
	}
	if g.JSON {
		return json.NewEncoder(g.Stdout).Encode(rows)
	}
	w := tabwriter.NewWriter(g.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDR\tDEVICE ID\tSTATE\tLAST SEEN\tCURSOR\tLAST ERROR")
	for _, p := range rows {
		seen := "never"
		if !p.LastSeen.IsZero() {
			seen = max(time.Duration(0), now.Sub(p.LastSeen)).Round(time.Second).String() + " ago"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s:%d\t%s\n", p.Name, p.Addr, p.DeviceID, p.State, seen, p.FeedEpoch, p.FeedSeq, p.LastError)
	}
	return w.Flush()
}

type peerStatus struct {
	fleet.Peer
	LastSeen  time.Time `json:"last_seen_at"`
	LastError string    `json:"last_error"`
	FeedEpoch string    `json:"feed_epoch"`
	FeedSeq   int64     `json:"feed_seq"`
}

func (v peerStatus) MarshalJSON() ([]byte, error) {
	return jsonx.Object(v.Peer, struct {
		LastSeen  time.Time `json:"last_seen_at"`
		LastError string    `json:"last_error"`
		FeedEpoch string    `json:"feed_epoch"`
		FeedSeq   int64     `json:"feed_seq"`
	}{v.LastSeen, v.LastError, v.FeedEpoch, v.FeedSeq})
}

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
)

// ServeCmd runs a foreground collector with fleet serving enabled.
type ServeCmd struct {
	Listen string `help:"Explicit private IP or interface and port (or set fleet.listen)."`
}

// Run loads configuration and shares the collector's writer lock and lifecycle.
func (c ServeCmd) Run(g *Globals) error {
	applyCollectorMemoryLimit()
	overrides := map[string]any{"fleet.serve": true}
	if c.Listen != "" {
		overrides["fleet.listen"] = c.Listen
	}
	cfg, err := config.LoadWithOverrides(config.Path(), overrides)
	if err != nil {
		return err
	}
	if _, err := fleet.CheckListen(cfg.Fleet.Listen, cfg.Fleet.AllowAnyAddress); err != nil {
		return err
	}
	d, err := NewDeps(cfg)
	if err != nil {
		return err
	}
	ctx, stop := notifyContext(context.Background())
	defer stop()
	restore, err := collectorLogging()
	if err != nil {
		return err
	}
	defer restore()
	return collector.Run(ctx, d, config.DataDir())
}

// FleetCmd groups local fleet inspection commands.
type FleetCmd struct {
	Enroll  FleetEnrollCmd  `cmd:"" help:"Open a bounded pairing window."`
	Add     FleetAddCmd     `cmd:"" help:"Pair with a serving machine."`
	Revoke  FleetRevokeCmd  `cmd:"" help:"Revoke a viewer immediately."`
	Remove  FleetRemoveCmd  `cmd:"" help:"Remove a peer locally."`
	Rekey   FleetRekeyCmd   `cmd:"" help:"Regenerate identity; invalidate all pairings."`
	Peers   FleetPeersCmd   `cmd:"" help:"List paired machines."`
	ID      FleetIDCmd      `cmd:"" help:"Print this machine's fleet device ID."`
	Status  FleetStatusCmd  `cmd:"" help:"Show the serving collector's live state."`
	Viewers FleetViewersCmd `cmd:"" help:"List identities trusted to read this machine."`
}

// FleetIDCmd loads or creates the local identity.
type FleetIDCmd struct{}

// Run prints the device ID, generating an identity on first use.
func (FleetIDCmd) Run(g *Globals) error {
	id, err := fleet.LoadOrCreateIdentity(filepath.Join(config.DataDir(), "fleet"))
	if err != nil {
		return err
	}
	if g.JSON {
		return json.NewEncoder(g.Stdout).Encode(map[string]string{"device_id": id.DeviceID})
	}
	_, err = fmt.Fprintln(g.Stdout, id.DeviceID)
	return err
}

// FleetStatusCmd queries the running collector's control socket.
type FleetStatusCmd struct{}

// Run prints live stream, presence and resnapshot counts.
func (FleetStatusCmd) Run(g *Globals) error {
	resp, err := fleet.Control(context.Background(), config.DataDir(), fleet.ControlRequest{Op: "status"})
	if err != nil {
		return fmt.Errorf("fleet collector unavailable: %w", err)
	}
	if g.JSON {
		return json.NewEncoder(g.Stdout).Encode(resp.Status)
	}
	s := resp.Status
	_, err = fmt.Fprintf(g.Stdout, "%s · %s\n%d streams · %d watching · %d connections · %d resnapshots\nenrollment open: %t\n", s.DeviceID, s.Listen, s.Streams, s.Presence, s.Connections, s.Resnapshots, s.EnrollmentOpen)
	return err
}

// FleetViewersCmd lists the persisted viewer trust entries.
type FleetViewersCmd struct{}

// Run prints viewer identities and granted permissions without opening a port.
func (FleetViewersCmd) Run(g *Globals) error {
	viewers, err := fleet.LoadViewers(filepath.Join(config.DataDir(), "fleet"))
	if err != nil {
		return err
	}
	if viewers == nil {
		viewers = []fleet.Viewer{}
	}
	if g.JSON {
		return json.NewEncoder(g.Stdout).Encode(viewers)
	}
	for _, v := range viewers {
		if _, err := fmt.Fprintf(g.Stdout, "%s\t%s\t%v\n", v.Name, v.DeviceID, v.Permissions); err != nil {
			return err
		}
	}
	return nil
}

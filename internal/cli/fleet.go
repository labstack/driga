package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/fleet"
)

// FleetCmd groups local fleet inspection commands.
type FleetCmd struct {
	Peers   FleetPeersCmd   `cmd:"" help:"List paired machines."`
	Viewers FleetViewersCmd `cmd:"" help:"List identities trusted to read this machine."`
	Enroll  FleetEnrollCmd  `cmd:"" help:"Open a bounded pairing window."`
	Rekey   FleetRekeyCmd   `cmd:"" help:"Regenerate identity; invalidate all pairings."`
	ID      FleetIDCmd      `cmd:"" help:"Print this machine's fleet device ID."`
	Status  FleetStatusCmd  `cmd:"" help:"Show the serving Driga service's live state."`
}

// FleetIDCmd loads or creates the local identity.
type FleetIDCmd struct{ JSONOptions }

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
type FleetStatusCmd struct{ JSONOptions }

// Run prints live stream, presence and resnapshot counts.
func (FleetStatusCmd) Run(g *Globals) error {
	resp, err := fleet.Control(context.Background(), config.DataDir(), fleet.ControlRequest{Op: "status"})
	if err != nil {
		return fmt.Errorf("the Driga service is unavailable: %w", err)
	}
	if g.JSON {
		return json.NewEncoder(g.Stdout).Encode(resp.Status)
	}
	s := resp.Status
	_, err = fmt.Fprintf(g.Stdout, "%s · %s\n%d streams · %d watching · %d connections · %d resnapshots\nenrollment open: %t\n", s.DeviceID, s.Listen, s.Streams, s.Presence, s.Connections, s.Resnapshots, s.EnrollmentOpen)
	return err
}

// FleetViewersCmd lists the persisted viewer trust entries.
type FleetViewersCmd struct {
	List   FleetViewersListCmd `cmd:"" help:"List identities trusted to read this machine."`
	Revoke FleetRevokeCmd      `cmd:"" help:"Revoke a viewer immediately."`
}
type FleetViewersListCmd struct{ JSONOptions }

// Run prints viewer identities and granted permissions without opening a port.
func (FleetViewersListCmd) Run(g *Globals) error {
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

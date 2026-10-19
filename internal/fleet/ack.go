package fleet

import (
	"fmt"
	"path/filepath"
)

// CheckAckPermission checks the grant saved at pairing without contacting a peer.
func CheckAckPermission(dataDir, machineID string) error {
	peers, err := LoadPeers(filepath.Join(dataDir, "fleet"))
	if err != nil {
		return err
	}
	name := machineID
	for _, p := range peers {
		if p.MachineID != machineID {
			continue
		}
		if p.Name != "" {
			name = p.Name
		}
		if p.Ack && p.State != "untrusted" {
			return nil
		}
		break
	}
	return fmt.Errorf("no ack permission on %s (re-pair with --ack)", name)
}

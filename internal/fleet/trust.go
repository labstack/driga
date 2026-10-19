package fleet

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var trustMu sync.Mutex

// Peer is a machine whose feed this collector follows.
type Peer struct {
	// Ack records the permission granted to this viewer during pairing.
	Ack       bool      `json:"ack,omitempty"`
	Name      string    `json:"name"`
	State     string    `json:"state,omitempty"`
	Addr      string    `json:"addr"`
	DeviceID  string    `json:"device_id"`
	MachineID string    `json:"machine_id"`
	Hostname  string    `json:"hostname"`
	OS        string    `json:"os"`
	PairedAt  time.Time `json:"paired_at"`
}

// Viewer is an identity authorized to connect to this collector.
type Viewer struct {
	Name          string    `json:"name"`
	DeviceID      string    `json:"device_id"`
	MachineID     string    `json:"machine_id"`
	Permissions   []string  `json:"permissions"`
	PairedAt      time.Time `json:"paired_at"`
	LastConnected time.Time `json:"last_connected"`
	EnrolledFrom  string    `json:"enrolled_from"`
}

// LoadPeers reads peers.json in the fleet directory; a missing file is empty.
func LoadPeers(dir string) ([]Peer, error) {
	return loadTrust[Peer](filepath.Join(dir, "peers.json"))
}

// SavePeers atomically replaces peers.json in the fleet directory, mode 0600.
func SavePeers(dir string, peers []Peer) error {
	return saveTrust(filepath.Join(dir, "peers.json"), peers)
}

// LoadViewers reads viewers.json in the fleet directory; a missing file is empty.
func LoadViewers(dir string) ([]Viewer, error) {
	return loadTrust[Viewer](filepath.Join(dir, "viewers.json"))
}

// SaveViewers atomically replaces viewers.json in the fleet directory, mode 0600.
func SaveViewers(dir string, viewers []Viewer) error {
	return saveTrust(filepath.Join(dir, "viewers.json"), viewers)
}

func loadTrust[T any](path string) ([]T, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []T
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("trust file %s: %w", path, err)
	}
	return entries, nil
}

func saveTrust[T any](path string, entries []T) error {
	if entries == nil {
		entries = []T{}
	}
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return atomicPrivateWrite(path, append(b, '\n'))
}

func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("fleet directory %s is not a directory", dir)
	}
	return os.Chmod(dir, 0700)
}

// atomicPrivateWrite never exposes a partly written destination or follows a
// destination symlink. The temporary file lives on the same filesystem.
func atomicPrivateWrite(path string, data []byte) error {
	if err := privateDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

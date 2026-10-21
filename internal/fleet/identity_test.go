package fleet_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/fleet"
	"github.com/labstack/driga/internal/store"
)

func newIdentity(t *testing.T) (fleet.Identity, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "fleet")
	id, err := fleet.LoadOrCreateIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	return id, dir
}

func TestIdentityCreatedOnceMode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	id, dir := newIdentity(t)
	second, err := fleet.LoadOrCreateIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(id.DER, second.DER) || id.DeviceID != second.DeviceID {
		t.Fatal("identity changed")
	}
	for _, name := range []string{"identity.key", "identity.crt"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode %o", name, info.Mode().Perm())
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("directory mode %o", info.Mode().Perm())
	}
	cert, err := x509.ParseCertificate(id.DER)
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.CheckSignature(cert.SignatureAlgorithm, cert.RawTBSCertificate, cert.Signature); err != nil {
		t.Fatal(err)
	}
	key, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		t.Fatalf("wrong public key: %T", cert.PublicKey)
	}
	if cert.NotAfter.Year() != cert.NotBefore.Year()+10 {
		t.Fatal("not a ten-year certificate")
	}
	if len(cert.ExtKeyUsage) != 2 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth || cert.ExtKeyUsage[1] != x509.ExtKeyUsageClientAuth {
		t.Fatal(cert.ExtKeyUsage)
	}
	st, err := store.Open(filepath.Join(filepath.Dir(dir), "driga.db"), store.ReadOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if cert.Subject.CommonName != st.MachineID() {
		t.Fatalf("CN %q != machine id %q", cert.Subject.CommonName, st.MachineID())
	}
	fp, err := fleet.ParseDeviceID(id.DeviceID)
	if err != nil || fp != sha256.Sum256(id.DER) {
		t.Fatal("bad fingerprint", err)
	}
}

func TestServeRefusesReadableKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	_, dir := newIdentity(t)
	path := filepath.Join(dir, "identity.key")
	for _, mode := range []os.FileMode{0640, 0604, 0644} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := fleet.LoadOrCreateIdentity(dir); err == nil {
			t.Fatalf("accepted key mode %o", mode)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != mode {
			t.Fatal("loader silently repaired insecure key")
		}
	}
}

func TestIdentityRejectsPartialAndCorrupt(t *testing.T) {
	for _, name := range []string{"identity.key", "identity.crt"} {
		t.Run(name, func(t *testing.T) {
			id, dir := newIdentity(t)
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			if _, err := fleet.LoadOrCreateIdentity(dir); err == nil {
				t.Fatal("partial identity replaced")
			}
			if err := os.WriteFile(filepath.Join(dir, name), []byte("corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := fleet.LoadOrCreateIdentity(dir); err == nil {
				t.Fatal("corrupt identity replaced")
			}
			if name == "identity.key" {
				b, _ := os.ReadFile(filepath.Join(dir, "identity.crt"))
				if !bytes.Contains(b, []byte("CERTIFICATE")) {
					t.Fatal(id.DeviceID)
				}
			}
		})
	}
}

func TestIdentityUsesExistingMachineID(t *testing.T) {
	data := t.TempDir()
	st, err := store.Open(filepath.Join(data, "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	machineID := st.MachineID()
	defer st.Close()
	id, err := fleet.LoadOrCreateIdentity(filepath.Join(data, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(id.DER)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != machineID {
		t.Fatalf("CN = %q, want %q", cert.Subject.CommonName, machineID)
	}
}

func TestPinnedTLSRejectsUnknownCert(t *testing.T) {
	server, _ := newIdentity(t)
	client, _ := newIdentity(t)
	other, _ := newIdentity(t)
	serverFP := sha256.Sum256(server.DER)
	clientFP := sha256.Sum256(client.DER)
	acceptServer := func(fp [32]byte) bool { return fleet.EqualFingerprint(fp, serverFP) }
	acceptClient := func(fp [32]byte) bool { return fleet.EqualFingerprint(fp, clientFP) }
	sc := fleet.PinnedTLS(server, acceptClient)
	if sc.MinVersion != tls.VersionTLS13 || sc.ClientAuth != tls.RequireAnyClientCert || !sc.InsecureSkipVerify {
		t.Fatal("insecure TLS configuration")
	}
	for _, tc := range []struct {
		name   string
		id     fleet.Identity
		accept func([32]byte) bool
		want   bool
	}{
		{"pinned", client, acceptServer, true},
		{"unknown_client", other, acceptServer, false},
		{"unknown_server", client, func([32]byte) bool { return false }, false},
		{"no_verifier", client, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			a.SetDeadline(time.Now().Add(3 * time.Second))
			b.SetDeadline(time.Now().Add(3 * time.Second))
			s := tls.Server(a, sc)
			c := tls.Client(b, fleet.PinnedTLS(tc.id, tc.accept))
			done := make(chan error, 1)
			go func() {
				err := s.Handshake()
				a.Close()
				done <- err
			}()
			ce := c.Handshake()
			if ce == nil {
				// TLS 1.3 can finish on the client before the server rejects its
				// certificate. Drain the alert so net.Pipe does not await a deadline.
				_, _ = c.Read(make([]byte, 1))
			}
			se := <-done
			if tc.want && (ce != nil || se != nil) {
				t.Fatalf("client %v; server %v", ce, se)
			}
			if !tc.want {
				err := ce
				if tc.name == "unknown_client" {
					err = se
				}
				if err == nil || !strings.Contains(err.Error(), "untrusted fleet peer certificate") {
					t.Fatalf("missing certificate rejection: client %v; server %v", ce, se)
				}
			}
		})
	}
	if err := sc.VerifyPeerCertificate(nil, nil); err == nil {
		t.Fatal("missing certificate accepted")
	}
	if err := sc.VerifyPeerCertificate([][]byte{[]byte("bad")}, nil); err == nil {
		t.Fatal("malformed certificate accepted")
	}
}

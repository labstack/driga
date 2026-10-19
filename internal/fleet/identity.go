// Package fleet manages certificate identities and pinned fleet trust.
package fleet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/labstack/driga/internal/store"
)

// Identity is this machine's certificate and its encoded DER fingerprint.
type Identity struct {
	Cert     tls.Certificate
	DER      []byte
	DeviceID string
}

var identityMu sync.Mutex

// LoadOrCreateIdentity loads identity.key and identity.crt from a fleet directory.
// On first use it binds the certificate CN to the parent data directory's
// driga.db machine_id, initializing that database only if it does not exist.
// An incomplete identity or a group/world readable key is an error, never a rekey.
func LoadOrCreateIdentity(dir string) (Identity, error) {
	identityMu.Lock()
	defer identityMu.Unlock()
	if _, err := os.Lstat(filepath.Join(dir, "reset_pending")); !errors.Is(err, fs.ErrNotExist) {
		return Identity{}, fmt.Errorf("identity reset incomplete; run driga machine reset-id again")
	}
	if err := privateDir(dir); err != nil {
		return Identity{}, err
	}
	keyPath, certPath := filepath.Join(dir, "identity.key"), filepath.Join(dir, "identity.crt")
	keyInfo, keyErr := os.Lstat(keyPath)
	certInfo, certErr := os.Lstat(certPath)
	if keyErr == nil && certErr == nil {
		if !keyInfo.Mode().IsRegular() || !certInfo.Mode().IsRegular() {
			return Identity{}, fmt.Errorf("fleet identity files must be regular files")
		}
		if keyInfo.Mode().Perm()&0044 != 0 {
			return Identity{}, fmt.Errorf("fleet identity key is group/world readable; require mode 0600")
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return Identity{}, fmt.Errorf("load fleet identity: %w", err)
		}
		return identityFromCert(cert)
	}
	if !errors.Is(keyErr, fs.ErrNotExist) || !errors.Is(certErr, fs.ErrNotExist) {
		return Identity{}, fmt.Errorf("incomplete or inaccessible fleet identity: key: %v; certificate: %v", keyErr, certErr)
	}
	dbPath := filepath.Join(filepath.Dir(filepath.Clean(dir)), "driga.db")
	mode := store.ReadOnly
	if _, err := os.Stat(dbPath); errors.Is(err, fs.ErrNotExist) {
		mode = store.ReadWrite
	} else if err != nil {
		return Identity{}, err
	}
	st, err := store.Open(dbPath, mode)
	if err != nil {
		return Identity{}, fmt.Errorf("fleet machine identity: %w", err)
	}
	machineID := st.MachineID()
	if err := st.Close(); err != nil {
		return Identity{}, err
	}
	return createIdentity(dir, machineID)
}

func createIdentity(dir, machineID string) (Identity, error) {
	keyPath, certPath := filepath.Join(dir, "identity.key"), filepath.Join(dir, "identity.crt")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Identity{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return Identity{}, err
	}
	// Positive, nonzero serial without a retry or bias that affects security.
	serial.Add(serial, big.NewInt(1))
	now := time.Now().UTC().Truncate(time.Second)
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: machineID},
		NotBefore:             now,
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return Identity{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return Identity{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	// Persist the key first. A crash between the two renames fails closed on the
	// next load rather than silently changing a possibly observed fingerprint.
	if err := atomicPrivateWrite(keyPath, keyPEM); err != nil {
		return Identity{}, err
	}
	if err := atomicPrivateWrite(certPath, certPEM); err != nil {
		return Identity{}, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return Identity{}, err
	}
	return identityFromCert(cert)
}

func identityFromCert(cert tls.Certificate) (Identity, error) {
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return Identity{}, err
	}
	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return Identity{}, fmt.Errorf("fleet identity must use ECDSA P-256")
	}
	cert.Leaf = leaf
	return Identity{Cert: cert, DER: leaf.Raw, DeviceID: DeviceID(leaf.Raw)}, nil
}

// EqualFingerprint compares certificate fingerprints in constant time.
func EqualFingerprint(a, b [32]byte) bool {
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// PinnedTLS replaces CA/hostname verification with an explicit fingerprint policy
// in both TLS roles. The policy should use EqualFingerprint to compare pins.
// A nil policy rejects every peer. TLS resumption is disabled so each connection
// consults the current policy, including revocations.
func PinnedTLS(id Identity, accept func(fp [32]byte) bool) *tls.Config {
	return &tls.Config{
		MinVersion:             tls.VersionTLS13,
		Certificates:           []tls.Certificate{id.Cert},
		ClientAuth:             tls.RequireAnyClientCert,
		InsecureSkipVerify:     true, // Verification below is by certificate fingerprint.
		SessionTicketsDisabled: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("fleet peer did not present a certificate")
			}
			if _, err := x509.ParseCertificate(rawCerts[0]); err != nil {
				return fmt.Errorf("invalid fleet peer certificate: %w", err)
			}
			fp := sha256.Sum256(rawCerts[0])
			if accept == nil || !accept(fp) {
				return fmt.Errorf("untrusted fleet peer certificate")
			}
			return nil
		},
	}
}

package fleet

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsIdentityDefaultOwner(t *testing.T) {
	// Ordinary creation uses the token's default owner, which can be the
	// Administrators group on elevated CI. The final directory and key must
	// still be owned by, and accessible only to, the individual user.
	dir := filepath.Join(t.TempDir(), "fleet")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id, err := LoadOrCreateIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkPrivateWindowsObject(dir, true); err != nil {
		t.Fatal(err)
	}
	if err := checkPrivateWindowsObject(filepath.Join(dir, "identity.key"), false); err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreateIdentity(dir)
	if err != nil || again.DeviceID != id.DeviceID {
		t.Fatal("identity changed", err)
	}
}

func TestWindowsPrivateSocketDirectory(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "run", "test.sock")
	if err := ensureSocketDir(sock); err != nil {
		t.Fatal(err)
	}
	if err := checkSocketDir(sock); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(filepath.Dir(sock), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = protectWindowsDir(filepath.Dir(sock)) })
	if err := checkSocketDir(sock); err == nil {
		t.Fatal("shared socket directory accepted")
	}
}

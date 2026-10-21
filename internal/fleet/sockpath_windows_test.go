package fleet

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

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

package secfile

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateCreation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "private")
	if err := MkdirPrivate(dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Dir(dir), dir} {
		if err := CheckPrivate(path); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "token")
	if err := WritePrivate(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivate(path); err != nil {
		t.Fatal(err)
	}
	if err := WritePrivate(path, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "replacement" {
		t.Fatal(string(b), err)
	}
	if f, err := OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY); !os.IsExist(err) {
		if f != nil {
			f.Close()
		}
		t.Fatal("exclusive create did not reject existing file", err)
	}
	f, err := CreateTemp(dir, "key-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, f.Name()} {
		if err := CheckPrivate(p); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRejectOtherReadAccess(t *testing.T) {
	for _, principal := range []string{"WD", "BU", "AU", "S-1-5-21-100-200-300-1001"} {
		t.Run(principal, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secret")
			if err := WritePrivate(path, []byte("secret")); err != nil {
				t.Fatal(err)
			}
			u, err := windows.GetCurrentProcessToken().GetTokenUser()
			if err != nil {
				t.Fatal(err)
			}
			// Keep the user's full access so cleanup remains possible.
			sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + u.User.Sid.String() + ")(A;;GR;;;" + principal + ")")
			if err != nil {
				t.Fatal(err)
			}
			acl, _, err := sd.DACL()
			if err != nil {
				t.Fatal(err)
			}
			if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
				t.Fatal(err)
			}
			if err := CheckPrivate(path); err == nil {
				t.Fatal("read grant to another principal accepted")
			}
		})
	}
}

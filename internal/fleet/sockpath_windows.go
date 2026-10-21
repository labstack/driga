package fleet

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows AF_UNIX ignores chmod on the socket. Protect its parent with a
// protected DACL granting only the current user access, inherited by children.
func privateSocketSD() (*windows.SECURITY_DESCRIPTOR, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := u.User.Sid.String()
	return windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")")
}
func ensureWindowsSocketDir(sock string) error {
	dir := filepath.Dir(sock)
	_, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
			return err
		}
		// CreateDirectory applies the restrictive descriptor atomically.
		sd, err := privateSocketSD()
		if err != nil {
			return err
		}
		p, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			return err
		}
		sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
		if err := windows.CreateDirectory(p, &sa); err != nil && err != windows.ERROR_ALREADY_EXISTS {
			return err
		}
	} else if err != nil {
		return err
	}
	return checkWindowsSocketDir(sock)
}
func checkWindowsSocketDir(sock string) error {
	dir := filepath.Dir(sock)
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Windows socket parent must be a real private directory")
	}
	return checkPrivateWindowsObject(dir, true)
}

func checkPrivateWindowsObject(path string, directory bool) error {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || !windows.EqualSid(owner, u.User.Sid) {
		return fmt.Errorf("Windows private object must be owned by this user")
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		return fmt.Errorf("Windows private object must have an owner-only DACL")
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, 0, &ace); err != nil {
		return err
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !windows.EqualSid(sid, u.User.Sid) || ace.Mask&0x001f01ff /* FILE_ALL_ACCESS */ != 0x001f01ff /* FILE_ALL_ACCESS */ {
		return fmt.Errorf("Windows private object DACL must grant full access only to its owner")
	}
	if directory {
		if ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) != windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			return fmt.Errorf("Windows private directory ACL must protect children")
		}
		control, _, err := sd.Control()
		if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
			return fmt.Errorf("Windows private directory DACL must be protected")
		}
	}

	return nil
}

// Shared socket checks already verified the parent DACL. Socket ownership on
// Windows follows that directory rather than Unix uid/mode bits.
func ownedByMe(os.FileInfo) bool { return true }

func protectWindowsDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("private directory must be real")
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if !windows.EqualSid(owner, u.User.Sid) {
		return fmt.Errorf("private directory must be owned by this user")
	}
	desired, err := privateSocketSD()
	if err != nil {
		return err
	}
	acl, _, err := desired.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
func privateKey(path string, _ os.FileInfo) error { return checkPrivateWindowsObject(path, false) }

func makePrivateFile(path string) error {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + u.User.Sid.String() + "D:P(A;;FA;;;" + u.User.Sid.String() + ")")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, u.User.Sid, nil, acl, nil)
}

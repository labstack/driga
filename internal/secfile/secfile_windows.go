package secfile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privateSD(directory bool) (*windows.SECURITY_DESCRIPTOR, *windows.SID, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, nil, err
	}
	flags := ""
	if directory {
		flags = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("O:" + u.User.Sid.String() + "D:P(A;" + flags + ";FA;;;" + u.User.Sid.String() + ")(A;" + flags + ";FA;;;SY)(A;" + flags + ";FA;;;BA)")
	return sd, u.User.Sid, err
}

// OpenFile applies a protected DACL before any data is written or truncated.
// CREATE_NEW supplies the security descriptor atomically for new files.
func OpenFile(path string, flag int) (*os.File, error) {
	if flag&os.O_CREATE != 0 {
		sd, _, err := privateSD(false)
		if err != nil {
			return nil, err
		}
		p, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return nil, err
		}
		sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
		h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, &sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if err == nil {
			if err := windows.CloseHandle(h); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, windows.ERROR_FILE_EXISTS) && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) || flag&os.O_EXCL != 0 {
			return nil, &os.PathError{Op: "open", Path: path, Err: err}
		}
	}
	if err := Protect(path); err != nil {
		return nil, err
	}
	// Keep delete sharing for the lifetime of the writer, not just creation.
	// Legacy spool migration can unlink a hard-linked day file while a hook
	// finishes appending through its existing handle.
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(windows.GENERIC_READ)
	switch flag & (os.O_WRONLY | os.O_RDWR) {
	case os.O_WRONLY:
		access = windows.GENERIC_WRITE
	case os.O_RDWR:
		access = windows.GENERIC_READ | windows.GENERIC_WRITE
	}
	if flag&os.O_APPEND != 0 {
		if flag&os.O_TRUNC == 0 {
			access &^= windows.GENERIC_WRITE
		}
		access |= windows.FILE_APPEND_DATA | windows.FILE_WRITE_ATTRIBUTES | windows.FILE_WRITE_EA | windows.STANDARD_RIGHTS_WRITE | windows.SYNCHRONIZE
	}
	attrs := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	if flag&os.O_SYNC != 0 {
		attrs |= windows.FILE_FLAG_WRITE_THROUGH
	}
	h, err := windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, attrs, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	if flag&os.O_TRUNC != 0 {
		if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

func ProtectFile(f *os.File) error { return Protect(f.Name()) }

// MkdirPrivate creates each missing ancestor with a private inheritable DACL.
// Existing ancestors are untouched; the requested directory is protected.
func MkdirPrivate(path string) error {
	path = filepath.Clean(path)
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("private directory must be real: %s", path)
		}
		return Protect(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return &os.PathError{Op: "mkdir", Path: path, Err: os.ErrNotExist}
	}
	if _, err := os.Lstat(parent); os.IsNotExist(err) {
		if err := MkdirPrivate(parent); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	sd, _, err := privateSD(true)
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err := windows.CreateDirectory(p, &sa); err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return &os.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return CheckPrivate(path)
}

// Protect refuses links and foreign owners before replacing an existing DACL.
func Protect(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("private object must be a real file or directory: %s", path)
	}
	desired, user, err := privateSD(info.IsDir())
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if !windows.EqualSid(owner, user) {
		// Elevated CI tokens may create objects owned by their default owner
		// (Administrators). Accept only this token's exact default owner.
		var size uint32
		token := windows.GetCurrentProcessToken()
		if err := windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &size); err != windows.ERROR_INSUFFICIENT_BUFFER {
			return fmt.Errorf("read token default owner: %w", err)
		}
		buf := make([]byte, size)
		if err := windows.GetTokenInformation(token, windows.TokenOwner, &buf[0], size, &size); err != nil {
			return err
		}
		defaultOwner := *(**windows.SID)(unsafe.Pointer(&buf[0]))
		if !windows.EqualSid(owner, defaultOwner) {
			return fmt.Errorf("private object must be owned by this user: %s", path)
		}
	}
	acl, _, err := desired.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, user, nil, acl, nil)
}

// CheckPrivate fails closed on null, inherited, or unrecognized ACLs and on
// grants to anyone except the current user, SYSTEM, and Administrators.
func CheckPrivate(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("private object must be a real file or directory: %s", path)
	}
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return err
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil || !windows.EqualSid(owner, u.User.Sid) {
		return fmt.Errorf("private object must be owned by this user: %s", path)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("private object DACL must be protected: %s", path)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return fmt.Errorf("private object must have a DACL: %s", path)
	}
	userAccess := false
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("unrecognized private object ACE: %s", path)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		isUser := windows.EqualSid(sid, u.User.Sid)
		if ace.Mask != 0 && !isUser && !windows.EqualSid(sid, system) && !windows.EqualSid(sid, admins) {
			return fmt.Errorf("private object grants access to another principal: %s", path)
		}
		const fullAccess = 0x001f01ff // FILE_ALL_ACCESS
		if isUser && ace.Mask&fullAccess == fullAccess && ace.Header.AceFlags&windows.INHERIT_ONLY_ACE == 0 {
			if !info.IsDir() || ace.Header.AceFlags&(windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE) == windows.OBJECT_INHERIT_ACE|windows.CONTAINER_INHERIT_ACE {
				userAccess = true
			}
		}
	}
	if !userAccess {
		return fmt.Errorf("private object must grant its user full access (inheritable for directories): %s", path)
	}
	return nil
}

func createTemp(dir, pattern string) (*os.File, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	if strings.ContainsAny(pattern, `/\`+string(filepath.Separator)) {
		return nil, &os.PathError{Op: "createtemp", Path: pattern, Err: os.ErrInvalid}
	}
	for range 100 {
		var nonce [8]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return nil, err
		}
		name := pattern + hex.EncodeToString(nonce[:])
		if i := strings.LastIndexByte(pattern, '*'); i >= 0 {
			name = pattern[:i] + hex.EncodeToString(nonce[:]) + pattern[i+1:]
		}
		f, err := OpenFile(filepath.Join(dir, name), os.O_RDWR|os.O_CREATE|os.O_EXCL)
		if !errors.Is(err, os.ErrExist) {
			return f, err
		}
	}
	return nil, &os.PathError{Op: "createtemp", Path: dir, Err: os.ErrExist}
}

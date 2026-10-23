package cleanup

import (
	"fmt"
	"golang.org/x/sys/windows"
)

// File IDs are immutable observations, never FileInfo's lazily loaded IDs.
type fileIdentity struct{ volume, indexHigh, indexLow uint32 }

func captureIdentity(path string) (*fileIdentity, error) {
	u, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(u, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, fmt.Errorf("recycle source is a reparse point")
	}
	return &fileIdentity{volume: info.VolumeSerialNumber, indexHigh: info.FileIndexHigh, indexLow: info.FileIndexLow}, nil
}
func sameIdentity(a, b *fileIdentity) bool { return a != nil && b != nil && *a == *b }

func validateRecycleIdentity(c Candidate) error {
	current, err := captureIdentity(c.Path)
	if err != nil {
		return err
	}
	if !sameIdentity(c.identity, current) {
		return fmt.Errorf("recycle source identity changed")
	}
	return nil
}

package command

import (
	"golang.org/x/sys/windows"
	"os"
)

func singleReadLink(f *os.File, _ os.FileInfo) bool {
	// Windows FileInfo.Sys has no link count. Query the already-open handle,
	// keeping the same inode pinned throughout the confined read.
	var info windows.ByHandleFileInformation
	return windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info) == nil && info.NumberOfLinks == 1
}

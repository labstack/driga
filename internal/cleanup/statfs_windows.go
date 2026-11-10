package cleanup

import "golang.org/x/sys/windows"

func freeBytes(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(p, &available, nil, nil); err != nil {
		return 0, err
	}
	return int64(available), nil
}

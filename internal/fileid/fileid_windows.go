package fileid

import (
	"encoding/binary"
	"hash/fnv"
	"os"

	"golang.org/x/sys/windows"
)

// Of needs an open file on Windows: FileInfo alone has no volume/file index.
func Of(v any) (uint64, bool) {
	f, ok := v.(*os.File)
	if !ok || f == nil {
		return 0, false
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &info); err != nil {
		return 0, false
	}
	var buf [12]byte
	binary.LittleEndian.PutUint32(buf[:4], info.VolumeSerialNumber)
	binary.LittleEndian.PutUint32(buf[4:8], info.FileIndexHigh)
	binary.LittleEndian.PutUint32(buf[8:], info.FileIndexLow)
	h := fnv.New64a()
	_, _ = h.Write(buf[:])
	// SQLite's checkpoint column is signed; keep all IDs representable.
	return h.Sum64() & (1<<63 - 1), true
}

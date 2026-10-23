package cleanup

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/labstack/driga/internal/pathx"
	"github.com/labstack/driga/internal/wincom"
	"golang.org/x/sys/windows"
)

var shell32 = windows.NewLazySystemDLL("shell32.dll")

func trash(ctx context.Context, _ Env, c Candidate, opts ApplyOpts, r Result) (Result, error) {
	r.Commands = [][]string{{"IFileOperation", "recycle", c.Path}}
	r.TrashPath = "shell:RecycleBinFolder" // Shell owns the destination name.
	if opts.DryRun {
		return r, nil
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	if !filepath.IsAbs(c.Path) {
		return r, fmt.Errorf("recycle path must be absolute and not redirected")
	}
	// Expand harmless 8.3 aliases before comparing with EvalSymlinks, which
	// expands aliases as well as reparse points. Keep the redirection refusal.
	u, err := windows.UTF16PtrFromString(c.Path)
	if err != nil {
		return r, err
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetLongPathName(u, &buf[0], uint32(len(buf)))
	if err != nil {
		return r, err
	}
	if n == 0 || n >= uint32(len(buf)) || !pathx.Equal(resolved(c.Path), windows.UTF16ToString(buf[:n])) {
		return r, fmt.Errorf("recycle path must be absolute and not redirected")
	}
	if err := validateRecycleIdentity(c); err != nil {
		return r, err
	}
	return r, recycle(ctx, c)
}

// IFileOperation must run synchronously on an STA. Never use SHFileOperation,
// RemoveAll or a permanent-delete fallback. The progress sink vetoes deletes
// when the Shell does not promise recycling and checks the per-item result.
func recycle(ctx context.Context, c Candidate) error {
	unlock, err := pinRecycleParents(c.Path)
	if err != nil {
		return err
	}
	defer unlock()
	done, err := wincom.InitializeSTA()
	if err != nil {
		return err
	}
	defer done()
	sink := newRecycleSink(ctx)
	sink.validate = func() error { return validateRecycleIdentity(c) }
	op, err := wincom.Create("{3AD05575-8857-4850-9277-11B85BDB8E09}", "{947AAB5F-0A5C-4C13-B4D6-4BF7836FC9F8}")
	if err != nil {
		return err
	}
	defer func() { op.Release(); runtime.KeepAlive(sink) }()
	if err := op.Call(5, uintptr(recycleFlags())); err != nil {
		return err
	} // SetOperationFlags
	u, err := windows.UTF16PtrFromString(c.Path)
	if err != nil {
		return err
	}
	iid, _ := windows.GUIDFromString("{43826D1E-E718-42EE-BC55-A1E261C37BFE}")
	var item *wincom.Object
	hr, _, _ := shell32.NewProc("SHCreateItemFromParsingName").Call(uintptr(unsafe.Pointer(u)), 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&item)))
	if err := wincom.Check(hr); err != nil {
		return err
	}
	defer item.Release()
	if err := op.Call(18, uintptr(unsafe.Pointer(item)), uintptr(unsafe.Pointer(sink))); err != nil {
		return err
	} // DeleteItem
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := op.Call(21); err != nil {
		return err
	} // PerformOperations
	var aborted int32
	if err := op.Call(22, uintptr(unsafe.Pointer(&aborted))); err != nil {
		return err
	}
	if sink.err != nil {
		return sink.err
	}
	if aborted != 0 || !sink.recycled {
		return fmt.Errorf("Recycle Bin operation aborted or not confirmed")
	}
	return nil
}

type recycleSink struct {
	vtable   *[19]uintptr
	ctx      context.Context
	err      error
	recycled bool
	validate func() error
}

var recycleSinkTable = [19]uintptr{
	windows.NewCallback(func(s *recycleSink, iid *windows.GUID, out *unsafe.Pointer) uintptr {
		unknown, _ := windows.GUIDFromString("{00000000-0000-0000-C000-000000000046}")
		progress, _ := windows.GUIDFromString("{04B0F1A7-9490-44BC-96E1-4296A31252E2}")
		*out = nil
		if *iid != unknown && *iid != progress {
			return 0x80004002
		}
		*out = unsafe.Pointer(s)
		return 0
	}),
	windows.NewCallback(func(*recycleSink) uintptr { return 2 }),
	windows.NewCallback(func(*recycleSink) uintptr { return 1 }),
	windows.NewCallback(func(*recycleSink) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(s *recycleSink, flags uint32, _ uintptr) uintptr {
		return s.preDelete(flags)
	}),
	windows.NewCallback(func(s *recycleSink, _ uintptr, _ uintptr, hr uintptr, recycled uintptr) uintptr {
		if err := wincom.Check(hr); err != nil {
			s.err = err
			return hr
		}
		if recycled == 0 {
			s.err = fmt.Errorf("Shell did not confirm a Recycle Bin item")
			return 0x80004005
		}
		s.recycled = true
		return 0
	}),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink, uintptr, uintptr) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink) uintptr { return 0 }),
	windows.NewCallback(func(*recycleSink) uintptr { return 0 }),
}

func (s *recycleSink) preDelete(flags uint32) uintptr {
	if err := s.ctx.Err(); err != nil {
		s.err = err
		return 0x80004004
	}
	if flags&0x80 == 0 {
		s.err = fmt.Errorf("Shell refused recycling; permanent deletion vetoed")
		return 0x80004004
	} // TSF_DELETE_RECYCLE_IF_POSSIBLE
	if s.validate != nil {
		if err := s.validate(); err != nil {
			s.err = err
			return 0x80004004
		}
	}
	return 0
}

func newRecycleSink(ctx context.Context) *recycleSink {
	return &recycleSink{vtable: &recycleSinkTable, ctx: ctx}
}

// Pin the real parent chain against rename/deletion while the Shell resolves
// its path. Reparse points (including junctions) cannot redirect authority.
func pinRecycleParents(path string) (func(), error) {
	var handles []windows.Handle
	closeAll := func() {
		for _, h := range handles {
			windows.CloseHandle(h)
		}
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		u, err := windows.UTF16PtrFromString(dir)
		if err != nil {
			closeAll()
			return nil, err
		}
		h, err := windows.CreateFile(u, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			closeAll()
			return nil, err
		}
		handles = append(handles, h)
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(h, &info); err != nil {
			closeAll()
			return nil, err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			closeAll()
			return nil, fmt.Errorf("recycle parent is a reparse point")
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return closeAll, nil
}

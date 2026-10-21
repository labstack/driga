//go:build !windows

// Package fileid supplies a stable local file identity for tail checkpoints.
package fileid

import (
	"os"
	"syscall"
)

// Of accepts os.FileInfo or *os.File. False means identity is unavailable.
func Of(v any) (uint64, bool) {
	var info os.FileInfo
	switch v := v.(type) {
	case *os.File:
		if v == nil {
			return 0, false
		}
		var err error
		info, err = v.Stat()
		if err != nil {
			return 0, false
		}
	case os.FileInfo:
		info = v
	default:
		return 0, false
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return s.Ino, true
}

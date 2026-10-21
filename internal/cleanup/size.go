package cleanup

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// WalkSize counts regular-file bytes without following symlinks or mount points.
// Deadline/cancellation produces a lower bound; callers display it with ≥.
func WalkSize(ctx context.Context, path string) (size int64, capped bool, err error) {
	return walkStatsWithPath(ctx, path, devicePath, nil)
}

// walkSize allows mount boundaries to be tested without mounting a real FS.
func walkSize(ctx context.Context, path string, device func(os.FileInfo) (uint64, bool)) (size int64, capped bool, err error) {
	return walkStats(ctx, path, device, nil)
}
func walkStats(ctx context.Context, path string, device func(os.FileInfo) (uint64, bool), visit func(os.FileInfo)) (size int64, capped bool, err error) {
	return walkStatsWithPath(ctx, path, func(_ string, info os.FileInfo) (uint64, bool) { return device(info) }, visit)
}

func walkStatsWithPath(ctx context.Context, path string, device func(string, os.FileInfo) (uint64, bool), visit func(os.FileInfo)) (size int64, capped bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := os.Lstat(path)
	if err != nil {
		return 0, false, err
	}
	base, ok := device(path, info)
	if !ok {
		return 0, false, fmt.Errorf("filesystem device identity unavailable")
	}
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			capped = true
			return fs.SkipAll
		}
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		stat, ok := device(p, info)
		if !ok {
			return fmt.Errorf("filesystem device identity unavailable")
		}
		if base != stat {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if info.Mode().IsRegular() {
			size += info.Size()
			if visit != nil {
				visit(info)
			}
		}
		return nil
	})
	return size, capped, err
}

// Measure is on-demand presentation IO, shared by CLI --sizes and TUI detail.
func Measure(ctx context.Context, c Candidate, now time.Time) (Candidate, error) {
	c.Ignored = append([]IgnoredPath(nil), c.Ignored...)
	c.Evidence = append([]string(nil), c.Evidence...)
	var err error
	if c.Kind == History {
		var buckets [3]int64
		c.Size, c.SizeCapped, err = walkStatsWithPath(ctx, c.Path, devicePath, func(info os.FileInfo) {
			age := now.Sub(info.ModTime())
			bucket := 0
			if age >= 30*24*time.Hour {
				bucket = 2
			} else if age >= 7*24*time.Hour {
				bucket = 1
			}
			buckets[bucket] += info.Size()
		})
		prefix := ""
		if c.SizeCapped {
			prefix = "≥"
		}
		c.Evidence = append(c.Evidence, fmt.Sprintf("age by file mtime: 0-7d: %s%d bytes; 7-30d: %s%d bytes; 30d+: %s%d bytes", prefix, buckets[0], prefix, buckets[1], prefix, buckets[2]))
	} else {
		c.Size, c.SizeCapped, err = WalkSize(ctx, c.Path)
	}
	for i := range c.Ignored {
		var capped bool
		var ignoredErr error
		c.Ignored[i].Size, capped, ignoredErr = WalkSize(ctx, filepath.Join(c.Path, c.Ignored[i].Path))
		if capped || ignoredErr != nil {
			c.Evidence = append(c.Evidence, "ignored size incomplete: "+c.Ignored[i].Path)
		}
	}
	return c, err
}

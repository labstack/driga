package platform

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"
)

const (
	rmFileLimit = 10000
	rmBatchSize = 100
	rmTimeLimit = 5 * time.Second
)

// restartManager is deliberately independent of the Windows ABI so bounded
// enumeration and fail-closed behavior can be tested on every platform.
type restartManager interface {
	Register([]string) error
	InUse() (bool, error)
	End() error
}

func restartManagerFiles(ctx context.Context, path string, limit, batchSize int, start func() (restartManager, error)) (open bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, rmTimeLimit)
	defer cancel()
	if limit <= 0 || batchSize <= 0 {
		return false, fmt.Errorf("invalid open-file inspection limits")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	var files []string
	entries := 0
	err = filepath.WalkDir(path, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Bound empty directory trees too, not just the registered file count.
		entries++
		if entries > limit+1 {
			return fmt.Errorf("open-file inspection exceeds %d entries", limit)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cannot inspect non-regular file %q", name)
		}
		files = append(files, name)
		if len(files) > limit {
			return fmt.Errorf("open-file inspection exceeds %d files", limit)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if len(files) == 0 {
		return false, nil
	}
	rm, err := start()
	if err != nil {
		return false, err
	}
	defer func() {
		err = errors.Join(err, rm.End(), ctx.Err())
		if err != nil {
			open = false
		}
	}()
	for offset := 0; offset < len(files); offset += batchSize {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := rm.Register(files[offset:min(offset+batchSize, len(files))]); err != nil {
			return false, err
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	open, err = rm.InUse()
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return open, nil
}

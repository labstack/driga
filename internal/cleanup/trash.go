package cleanup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func trash(ctx context.Context, e Env, c Candidate, opts ApplyOpts, r Result) (Result, error) {
	files, infoDir, err := trashDirs(e.Home)
	if err != nil {
		return r, err
	}
	canonicalHome, err := filepath.EvalSymlinks(e.Home)
	if err != nil {
		return r, err
	}
	validateDirs := func() error {
		for _, dir := range []string{files, infoDir} {
			if dir == "" {
				continue
			}
			rel, err := filepath.Rel(e.Home, dir)
			if err != nil || resolved(dir) != filepath.Join(canonicalHome, rel) {
				return fmt.Errorf("trash directory is redirected by a symlink")
			}
		}
		return nil
	}
	if err := validateDirs(); err != nil {
		return r, err
	}
	name := filepath.Base(c.Path)
	for i := 0; ; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d.%d", name, e.now().UnixNano(), i)
		}
		r.TrashPath = filepath.Join(files, candidate)
		_, err := os.Lstat(r.TrashPath)
		if os.IsNotExist(err) {
			if infoDir != "" {
				if _, err := os.Lstat(filepath.Join(infoDir, candidate+".trashinfo")); err == nil {
					continue
				} else if !os.IsNotExist(err) {
					return r, err
				}
			}
			break
		}
		if err != nil {
			return r, err
		}
	}
	r.Commands = [][]string{{"rename", c.Path, r.TrashPath}}
	if opts.DryRun {
		return r, nil
	}
	if err := ctx.Err(); err != nil {
		return r, err
	}
	for _, dir := range []string{files, infoDir} {
		if dir != "" {
			if err := os.MkdirAll(dir, 0700); err != nil {
				return r, fmt.Errorf("trash unavailable: %w", err)
			}
		}
	}
	// OS trash locations must remain the real directories beneath HOME.
	// Resolve HOME's normal system aliases, but never redirect a trash subdir.
	if err := validateDirs(); err != nil {
		return r, err
	}
	// The OS rename is the sole move primitive. EXDEV refuses without copying.
	canonicalFiles := filepath.Join(canonicalHome, mustRelative(e.Home, files))
	canonicalInfo := ""
	if infoDir != "" {
		canonicalInfo = filepath.Join(canonicalHome, mustRelative(e.Home, infoDir))
	}
	finish, err := trashInfo(canonicalInfo, filepath.Base(r.TrashPath), c.Path, e.now())
	if err != nil {
		return r, err
	}
	var moveErr error
	if e.Rename != nil {
		moveErr = e.Rename(c.Path, r.TrashPath)
	} else {
		moveErr = renameScratch(c, filepath.Join(canonicalFiles, filepath.Base(r.TrashPath)))
	}
	if err := moveErr; err != nil {
		finish(false)
		return r, fmt.Errorf("trash rename (same filesystem required): %w", err)
	}
	finish(true)
	return r, nil
}

// Both arguments were already validated as known HOME-relative OS paths.
func mustRelative(home, path string) string { rel, _ := filepath.Rel(home, path); return rel }

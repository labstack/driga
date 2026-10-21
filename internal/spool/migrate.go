package spool

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/labstack/driga/internal/fileid"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/labstack/driga/internal/model"
)

var linkLegacySpool = os.Link

// Migrate copies legacy day files atomically, preserving consumed positions in
// the destination's offset namespace. Call under the collector writer lock,
// before draining the destination. Checkpoint before unlinking; a failed or
// interrupted copy/checkpoint leaves the source available for an idempotent retry.
// Hard links preserve in-flight append descriptors on the same filesystem;
// streamed copies support a quiescent legacy spool on another filesystem.
func Migrate(old, dest string, load func(string) (model.Offset, error), save func(string, model.Offset) error) error {
	entries, err := os.ReadDir(old)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0700); err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("legacy spool %s: expected regular file", entry.Name())
		}
		source := filepath.Join(old, entry.Name())
		targetName := "legacy-" + entry.Name()
		target := filepath.Join(dest, targetName)
		before, err := os.Stat(source)
		if err != nil {
			return err
		}
		prev, err := load(entry.Name())
		if err != nil {
			return err
		}
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			if err := linkLegacySpool(source, target); crossDevice(err) {
				if err := copySpoolFile(source, target); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if err := syncDir(dest); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		info, err := os.Lstat(target)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("legacy spool target is not regular: %s", target)
		}
		linked := os.SameFile(before, info)
		// An existing destination means an interrupted attempt or a collision.
		// Never overwrite it. Compare bounded-memory digests before checkpointing.
		if !linked {
			left, err := fileDigest(source)
			if err != nil {
				return err
			}
			right, err := fileDigest(target)
			if err != nil {
				return err
			}
			if left != right {
				return fmt.Errorf("legacy spool migration conflict: %s (source retained)", target)
			}
		}
		after, err := os.Stat(source)
		if err != nil {
			return err
		}
		if !os.SameFile(before, after) || (!linked && (before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()))) {
			return fmt.Errorf("legacy spool changed during migration: %s (retry after old hooks finish)", source)
		}
		info, err = os.Lstat(target)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("legacy spool target is not regular: %s", target)
		}
		targetID, err := spoolIdentity(target)
		if err != nil {
			return err
		}
		sourceID, err := spoolIdentity(source)
		if err != nil {
			return err
		}
		next := model.Offset{Inode: targetID, Size: info.Size(), Mtime: info.ModTime()}
		if prev.Inode == sourceID && prev.Pos <= before.Size() {
			next.Pos = prev.Pos
		}
		if err := save(targetName, next); err != nil {
			return err
		}
		if err := os.Remove(source); err != nil {
			return err
		}
		if err := syncDir(old); err != nil {
			return err
		}
	}
	note := []byte("Driga moved hook events to " + dest + ". Hooks resolve this path at runtime; no reinstall is needed.\nAny remaining diagnostic logs can be removed when no longer needed.\n")
	return os.WriteFile(filepath.Join(old, "README"), note, 0600)
}

func syncDir(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

func fileDigest(path string) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [32]byte{}, err
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

func copySpoolFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(target), ".legacy-*")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	_, copyErr := io.Copy(out, in)
	if err := errors.Join(copyErr, out.Sync(), out.Close()); err != nil {
		return err
	}
	// Link publishes without replacing a concurrent or unrelated destination.
	if err := os.Link(out.Name(), target); err != nil {
		return err
	}
	return syncDir(filepath.Dir(target))
}

func spoolIdentity(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	id, ok := fileid.Of(f)
	if !ok {
		return 0, fmt.Errorf("file identity unavailable: %s", path)
	}
	return id, nil
}

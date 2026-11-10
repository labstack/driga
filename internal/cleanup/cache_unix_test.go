//go:build !windows

package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheLocationUsesStdout(t *testing.T) {
	e, _, path := cacheFixture(t)
	e.Tool = nil
	bin := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in 'env GOCACHE') printf 'tool warning\\n' >&2; printf '%s\\n' '" + path + "';; *) exit 1;; esac\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, c := range cacheCandidates(context.Background(), e, snapshot{}) {
		if c.ID == "cache:go-build" && c.Path == path {
			return
		}
	}
	t.Fatal("stderr warning hid cache location from stdout")
}

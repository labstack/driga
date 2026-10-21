package collector

import (
	"runtime"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestMergeObservationRetainsRecordedPaths(t *testing.T) {
	for _, tc := range []struct{ cwd, file string }{
		{`D:\Work\子 folder`, `D:/Work/子 folder/A.go`},
		{`\\Server\Share\Work`, `\\Server\Share\Work\A.go`},
		{"/r", "/r/a.go"},
	} {
		t.Run(tc.cwd, func(t *testing.T) {
			src := model.SessionObs{Cwd: tc.cwd, FilesTouched: []model.FileTouch{{Path: tc.file}}}
			var dst model.SessionObs
			mergeObs(&dst, src)
			if dst.Cwd != tc.cwd || len(dst.FilesTouched) != 1 || dst.FilesTouched[0].Path != tc.file {
				t.Fatalf("recorded paths changed: %+v", dst)
			}
		})
	}
	if runtime.GOOS != "windows" {
		return // Native Unix symlink normalization is covered by the full fixture corpus.
	}
	var dst model.SessionObs
	mergeObs(&dst, model.SessionObs{Cwd: "/r", FilesTouched: []model.FileTouch{{Path: "/r/../a.go"}}})
	if dst.FilesTouched[0].Path != "/r/../a.go" {
		t.Fatal("Windows rewrote an absolute agent path", dst.FilesTouched)
	}
}

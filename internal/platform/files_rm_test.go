package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeRestartManager struct {
	batches                      [][]string
	open, ended                  bool
	registerErr, listErr, endErr error
	onRegister                   func()
}

func (r *fakeRestartManager) Register(files []string) error {
	r.batches = append(r.batches, append([]string(nil), files...))
	if r.onRegister != nil {
		r.onRegister()
	}
	return r.registerErr
}
func (r *fakeRestartManager) InUse() (bool, error) { return r.open, r.listErr }
func (r *fakeRestartManager) End() error           { r.ended = true; return r.endErr }

func TestRestartManagerFiles(t *testing.T) {
	dir := t.TempDir()
	for i := range 5 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprint(i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	boom := errors.New("Restart Manager failed")
	for _, tc := range []struct {
		name              string
		limit             int
		rm                fakeRestartManager
		startErr          error
		wantOpen, wantErr bool
	}{
		{name: "batches", limit: 5},
		{name: "in use including self", limit: 5, rm: fakeRestartManager{open: true}, wantOpen: true},
		{name: "cap", limit: 4, wantErr: true},
		{name: "start failure", limit: 5, startErr: boom, wantErr: true},
		{name: "register failure", limit: 5, rm: fakeRestartManager{registerErr: boom}, wantErr: true},
		{name: "list failure", limit: 5, rm: fakeRestartManager{listErr: boom}, wantErr: true},
		{name: "end failure", limit: 5, rm: fakeRestartManager{open: true, endErr: boom}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := false
			got, err := restartManagerFiles(context.Background(), dir, tc.limit, 2, func() (restartManager, error) { started = tc.startErr == nil; return &tc.rm, tc.startErr })
			if got != tc.wantOpen || (err != nil) != tc.wantErr {
				t.Fatalf("open=%t err=%v", got, err)
			}
			if started && !tc.rm.ended {
				t.Fatal("session leaked")
			}
			if tc.name == "cap" && started {
				t.Fatal("cap must fail before registering incomplete tree")
			}
			if tc.name == "batches" {
				var sizes []int
				for _, batch := range tc.rm.batches {
					sizes = append(sizes, len(batch))
					for _, name := range batch {
						if !filepath.IsAbs(name) {
							t.Fatal(name)
						}
					}
				}
				if !reflect.DeepEqual(sizes, []int{2, 2, 1}) {
					t.Fatal(sizes)
				}
			}
		})
	}
	t.Run("cancellation between batches", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rm := &fakeRestartManager{onRegister: cancel}
		_, err := restartManagerFiles(ctx, dir, 5, 2, func() (restartManager, error) { return rm, nil })
		if !errors.Is(err, context.Canceled) || len(rm.batches) != 1 || !rm.ended {
			t.Fatalf("%+v %v", rm, err)
		}
	})
	t.Run("expired deadline", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		_, err := restartManagerFiles(ctx, dir, 5, 2, func() (restartManager, error) { t.Fatal("started after deadline"); return nil, nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline: %v", err)
		}
	})

	for _, path := range []string{filepath.Join(dir, "missing"), dir} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := restartManagerFiles(ctx, path, 5, 2, func() (restartManager, error) { t.Fatal("started on incomplete walk"); return nil, nil }); err == nil {
			t.Fatal("incomplete walk returned no error")
		}
	}
}

func TestRestartManagerEmptyDirectoryCap(t *testing.T) {
	dir := t.TempDir()
	for i := range 4 {
		if err := os.Mkdir(filepath.Join(dir, fmt.Sprint(i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	start := func() (restartManager, error) { t.Fatal("empty tree started RM"); return nil, nil }
	if _, err := restartManagerFiles(context.Background(), dir, 3, 2, start); err == nil {
		t.Fatal("unbounded directories")
	}
	if open, err := restartManagerFiles(context.Background(), dir, 4, 2, start); open || err != nil {
		t.Fatalf("empty tree: %t %v", open, err)
	}
}

package platform

import (
	"errors"
	"slices"
	"testing"
)

func TestIdentCachePartialReadRetried(t *testing.T) {
	unavailable := errors.New("metadata unavailable")
	for _, tc := range []struct {
		name string
		argv []string
		exe  string
		err  error
	}{
		{"argv available", []string{"node", "fixture.js"}, "", unavailable},
		{"exe available", nil, "/fixture/node", unavailable},
		{"empty success", nil, "", nil},
		{"argv empty success", nil, "/fixture/node", nil},
		{"argv zero empty success", []string{""}, "/fixture/node", nil},
		{"exe empty success", []string{"node", "fixture.js"}, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cache identCache
			key := identKey{start: 123, comm: "node"}
			argv, exe, err := cache.get(42, key, func() ([]string, string, error) { return tc.argv, tc.exe, tc.err })
			if !slices.Equal(argv, tc.argv) || exe != tc.exe || !errors.Is(err, tc.err) {
				t.Errorf("partial metadata lost: argv=%v exe=%q err=%v", argv, exe, err)
			}
			calls := 0
			argv, exe, err = cache.get(42, key, func() ([]string, string, error) {
				calls++
				return []string{"node", "recovered.js"}, "/fixture/node", nil
			})
			if calls != 1 || err != nil || !slices.Equal(argv, []string{"node", "recovered.js"}) || exe != "/fixture/node" {
				t.Fatalf("incomplete identity not retried: calls=%d argv=%v exe=%q err=%v", calls, argv, exe, err)
			}
		})
	}
}

func TestIdentCacheImageInvalidation(t *testing.T) {
	var cache identCache
	key := identKey{start: 123, comm: "node"}
	loads := 0
	load := func() ([]string, string, error) { loads++; return []string{"node", "fixture.js"}, "/fixture/node", nil }
	for range 2 {
		if _, _, err := cache.get(42, key, load); err != nil {
			t.Fatal(err)
		}
	}
	if loads != 1 {
		t.Fatalf("complete identity reloaded %d times", loads)
	}
	for _, replacement := range []identKey{{start: 123, comm: "python"}, {start: 456, comm: "python"}} {
		argv, exe, err := cache.get(42, replacement, func() ([]string, string, error) { return nil, "/fixture/python", errors.New("argv unavailable") })
		if argv != nil || exe != "/fixture/python" || err == nil {
			t.Fatalf("old image survived: argv=%v exe=%q err=%v", argv, exe, err)
		}
		if _, _, err := cache.get(42, replacement, load); err != nil {
			t.Fatal(err)
		}
	}
	if loads != 3 {
		t.Fatalf("exec/PID reuse did not reload: %d", loads)
	}
	cache.retain(nil)
	if _, _, err := cache.get(42, identKey{start: 456, comm: "python"}, load); err != nil {
		t.Fatal(err)
	}
	if loads != 4 {
		t.Fatal("removed process retained")
	}
}

func TestReadIdentityIndependentFieldsAndImagePin(t *testing.T) {
	key := identKey{start: 123, comm: "node"}
	argvErr, exeErr, pinErr := errors.New("argv unavailable"), errors.New("exe unavailable"), errors.New("image unavailable")
	for _, tc := range []struct {
		name                    string
		argvErr, exeErr, pinErr error
		current                 identKey
		wantArgv, wantExe       bool
	}{
		{"both", nil, nil, nil, key, true, true},
		{"argv failed", argvErr, nil, nil, key, false, true},
		{"exe failed", nil, exeErr, nil, key, true, false},
		{"both failed", argvErr, exeErr, nil, key, false, false},
		{"exec during read", nil, nil, nil, identKey{start: key.start, comm: "python"}, false, false},
		{"PID reused during read", nil, nil, nil, identKey{start: 456, comm: key.comm}, false, false},
		{"pin unavailable", nil, nil, pinErr, key, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argvCalls, exeCalls, pinCalls := 0, 0, 0
			argv, exe, err := readIdentity(key,
				func() ([]string, error) { argvCalls++; return []string{"node", "fixture.js"}, tc.argvErr },
				func() (string, error) { exeCalls++; return "/fixture/node", tc.exeErr },
				func() (identKey, error) { pinCalls++; return tc.current, tc.pinErr })
			if argvCalls != 1 || exeCalls != 1 || pinCalls != 1 {
				t.Fatalf("independent reads skipped: %d/%d/%d", argvCalls, exeCalls, pinCalls)
			}
			if (len(argv) != 0) != tc.wantArgv || (exe != "") != tc.wantExe {
				t.Fatalf("identity availability: argv=%v exe=%q err=%v", argv, exe, err)
			}
			for _, wantErr := range []error{tc.argvErr, tc.exeErr, tc.pinErr} {
				if wantErr != nil && !errors.Is(err, wantErr) {
					t.Fatalf("lost error %v: %v", wantErr, err)
				}
			}
			if tc.current != key && !errors.Is(err, errIdentityChanged) {
				t.Fatalf("changed image not refused: %v", err)
			}
			if tc.name == "both" && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIdentCacheOldLoadCannotReplaceNewImage(t *testing.T) {
	var cache identCache
	oldKey, newKey := identKey{start: 123, comm: "node"}, identKey{start: 123, comm: "python"}
	loading, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = cache.get(42, oldKey, func() ([]string, string, error) {
			close(loading)
			<-release
			return []string{"node", "old.js"}, "/fixture/node", nil
		})
	}()
	<-loading
	_, _, _ = cache.get(42, newKey, func() ([]string, string, error) { return []string{"python", "new.py"}, "/fixture/python", nil })
	close(release)
	<-done
	loads := 0
	argv, exe, err := cache.get(42, newKey, func() ([]string, string, error) { loads++; return nil, "", nil })
	if loads != 0 || err != nil || !slices.Equal(argv, []string{"python", "new.py"}) || exe != "/fixture/python" {
		t.Fatalf("old read replaced new image: loads=%d argv=%v exe=%q err=%v", loads, argv, exe, err)
	}
}

func TestIdentCacheDoesNotMergePartialAttempts(t *testing.T) {
	var cache identCache
	key := identKey{start: 123, comm: "node"}
	_, _, _ = cache.get(42, key, func() ([]string, string, error) {
		return []string{"node", "first.js"}, "", errors.New("exe unavailable")
	})
	argv, exe, err := cache.get(42, key, func() ([]string, string, error) { return nil, "/fixture/node", errors.New("argv unavailable") })
	if argv != nil || exe != "/fixture/node" || err == nil {
		t.Fatalf("mixed partial observations: argv=%v exe=%q err=%v", argv, exe, err)
	}
}

package fileid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentity(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a, ok := Of(f)
	if !ok {
		t.Fatal("no identity")
	}
	if _, err := f.WriteString("append"); err != nil {
		t.Fatal(err)
	}
	g, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	if b, ok := Of(g); !ok || a != b {
		t.Fatal("identity changed on append/reopen")
	}
	h, err := os.Create(filepath.Join(t.TempDir(), "other"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if b, ok := Of(h); !ok || a == b {
		t.Fatal("different files share identity")
	}
	if _, ok := Of(nil); ok {
		t.Fatal("nil has identity")
	}
	f.Close()
	if _, ok := Of(f); ok {
		t.Fatal("closed file has identity")
	}
}

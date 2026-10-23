package platform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/mem"
)

func TestLinuxMissingPSI(t *testing.T) {
	// Missing PSI must also work through the file-reading path, as in containers.
	s, err := (procFS{root: procFixture(t)}).Machine(context.Background())
	if err != nil || s.PsiMemSome10 != nil {
		t.Fatalf("missing PSI = %+v, %v", s, err)
	}
}
func TestLinuxMachinePressureFree(t *testing.T) {
	root := procFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "pressure"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pressure/memory"), []byte("some avg10=1.23 avg60=0.00 total=123\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fs := procFS{root: root}
	s, err := fs.Machine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	swap, err := mem.SwapMemoryWithContext(fs.context(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	if s.PressureFree != 25 || s.MemTotal != 4096*1024 || s.MemUsed != 3072*1024 || s.SwapUsed != swap.Used || s.SwapTotal != swap.Total || s.PsiMemSome10 == nil || *s.PsiMemSome10 != 1.23 || s.Cores < 1 || s.DiskFree == 0 || s.TS.IsZero() {
		t.Fatalf("sample = %+v", s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fs.Machine(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestLinuxVMContainerWithoutPSI(t *testing.T) {
	root := "../../testdata/fixtures/mixed/heldout-v1/heldout/linux-vm-synthetic/proc"
	pid, ok := UserSubreaper(root, 1001)
	if !ok || pid != 700 {
		t.Fatal(pid, ok)
	}
	sample, err := (procFS{root: root}).Machine(context.Background())
	if err != nil || sample.PsiMemSome10 != nil || sample.PressureFree != 25 {
		t.Fatalf("%+v %v", sample, err)
	}
}

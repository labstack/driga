package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestServicePreservesScoopJunction(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "2026.10.8-alpha")
	link := filepath.Join(root, "current")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("junction: %v: %s", err, out)
	}
	bin := filepath.Join(link, "driga.exe")
	if err := os.WriteFile(filepath.Join(target, "driga.exe"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := serviceBinaryPath(bin)
	if err != nil || got != bin {
		t.Fatalf("stable executable path = %q, %v; want %q", got, err, bin)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatal(err)
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMachineDiskSetting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("[machine]\ndisk_free_min = '2GiB'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Machine.DiskFreeMin != 2<<30 {
		t.Fatal(c.Machine)
	}
	t.Setenv("DRIGA_MACHINE_DISK_FREE_MIN", "3000")
	c, err = Load(p)
	if err != nil || c.Machine.DiskFreeMin != 3000 {
		t.Fatalf("%+v %v", c.Machine, err)
	}
}

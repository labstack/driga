package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	ks "github.com/kardianos/service"
)

func TestWindowsTaskInstallUsesPrivateTemporaryXML(t *testing.T) {
	root := t.TempDir()
	for key, value := range map[string]string{"HOME": root, "USERPROFILE": root, "APPDATA": root, "LOCALAPPDATA": root, "DRIGA_DATA_DIR": root, "DRIGA_SPOOL_DIR": filepath.Join(root, "events"), "DRIGA_CONFIG": filepath.Join(root, "config.toml")} {
		t.Setenv(key, value)
	}
	s := &taskService{bin: filepath.Join(root, "driga.exe"), sid: "S-1-5-21-123", name: `\Driga-tests-unique\collector`, dataDir: root}
	var xmlPath string
	s.run = func(name string, args ...string) ([]byte, error) {
		if name != "schtasks.exe" || len(args) != 5 || !reflect.DeepEqual(args[:4], []string{"/Create", "/TN", s.name, "/XML"}) {
			t.Fatalf("%s %q", name, args)
		}
		xmlPath = args[4]
		b, err := os.ReadFile(xmlPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "LeastPrivilege") || !strings.Contains(string(b), s.sid) {
			t.Fatal(string(b))
		}
		return nil, nil
	}
	if err := s.Install(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(xmlPath); !os.IsNotExist(err) {
		t.Fatal("temporary XML retained", err)
	}
	// On failures the XML is removed too. This test never creates a real task.
	s.run = func(_ string, args ...string) ([]byte, error) {
		xmlPath = args[4]
		return nil, errors.New("scheduler fixture failure")
	}
	if err := s.Install(); err == nil {
		t.Fatal("lost scheduler failure")
	}
	if _, err := os.Stat(xmlPath); !os.IsNotExist(err) {
		t.Fatal("temporary XML retained after failure", err)
	}
}

func TestWindowsTaskStateMissingUniqueTestFolder(t *testing.T) {
	// Read-only COM integration. Never register a task or access the real task.
	name := fmt.Sprintf(`\Driga-tests-%d-%d\collector`, os.Getpid(), time.Now().UnixNano())
	state, err := (&taskService{name: name}).TaskState()
	if !errors.Is(err, ks.ErrNotInstalled) || state != "not installed" {
		t.Fatalf("%s: %s %v", name, state, err)
	}
}

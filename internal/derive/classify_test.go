package derive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/signatures"
)

func TestClassifierIdentityCommandAndPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.toml")
	if err := os.WriteFile(path, []byte("[[signature]]\nname='custom helper'\nrole='helper'\nmatch=['^codex$']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sigs, err := signatures.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	c := NewClassifier(sigs, 1)
	p := model.Proc{PID: 42, PPID: 1, StartTime: time.Unix(100, 0), Cmdline: "codex"}
	for range 3 {
		if sig, ok := c.Match(p); !ok || sig.Name != "custom helper" {
			t.Fatal("lost custom precedence", sig, ok)
		}
	}
	if len(c.cache) != 1 {
		t.Fatal("repeated match was not reused", len(c.cache))
	}
	idx := NewProcIndex([]model.Proc{p})
	for range 3 {
		orphans := FindOrphans(idx, nil, sigs, time.Unix(200, 0), 0, nil, WithClassifier(c))
		if len(orphans) != 1 || orphans[0].Signature != "custom helper" || len(c.cache) != 1 {
			t.Fatal("orphan pass lost shared custom match", orphans, len(c.cache))
		}
	}
	p.Cmdline = "claude"
	if sig, ok := c.Match(p); !ok || sig.Agent != "claude" {
		t.Fatal("exec with same PID reused old command", sig, ok)
	}
	p.Cmdline, p.Exe = "", "codex"
	if sig, ok := c.Match(p); !ok || sig.Name != "custom helper" {
		t.Fatal("exe fallback", sig, ok)
	}
	p.Exe = "unrelated"
	if _, ok := c.Match(p); ok {
		t.Fatal("changed executable reused a match")
	}
	p.StartTime = p.StartTime.Add(time.Second)
	c.Match(p)
	if len(c.cache) != 5 {
		t.Fatal("PID reuse did not create a fresh identity", len(c.cache))
	}
	if len(NewClassifier(sigs, 1).cache) != 0 {
		t.Fatal("matches leaked into a new pass")
	}
}

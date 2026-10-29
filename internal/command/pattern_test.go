package command

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrantPatternContainment(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	outside := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "one"), 0700)
	_ = os.Mkdir(filepath.Join(root, "two"), 0700)
	_ = os.Symlink(outside, filepath.Join(root, "escape"))
	_ = os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "dangling"))
	p := Prepared{Parsed: Parsed{Request: Request{Argv: []string{"git", "worktree", "remove", filepath.Join(root, "one")}, Cwd: root, TimeoutSeconds: 120}}, Scope: Scope{Project: root, Scratch: root + "/scratch"}, Policy: Policy{Class: Other, Commands: []Executable{{Path: "/usr/bin/git", Argv: []string{"git", "worktree", "remove", filepath.Join(root, "one")}, SHA256: [32]byte{1}}}}, Sandbox: Capability{Available: true}}
	pattern, err := NewArgvPattern(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		edit func(*Prepared)
		want bool
	}{
		{"bounded", func(p *Prepared) { p.Parsed.Request.Argv[3] = filepath.Join(root, "two") }, true},
		{"traversal", func(p *Prepared) { p.Parsed.Request.Argv[3] = root + "/one/../two" }, false},
		{"outside", func(p *Prepared) { p.Parsed.Request.Argv[3] = outside }, false},
		{"dangling symlink", func(p *Prepared) { p.Parsed.Request.Argv[3] = root + "/dangling/child" }, false},
		{"symlink", func(p *Prepared) { p.Parsed.Request.Argv[3] = root + "/escape" }, false},
		{"flags", func(p *Prepared) { p.Parsed.Request.Argv[2] = "remove --force" }, false},
		{"cwd", func(p *Prepared) { p.Parsed.Request.Cwd = outside }, false},
		{"network", func(p *Prepared) { p.Parsed.Request.Network = true }, false},
		{"class", func(p *Prepared) { p.Policy.Class = Destructive }, false},
		{"executable", func(p *Prepared) { p.Policy.Commands[0].SHA256 = [32]byte{2} }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cp := p
			cp.Parsed.Request.Argv = append([]string(nil), p.Parsed.Request.Argv...)
			cp.Policy.Commands = append([]Executable(nil), p.Policy.Commands...)
			test.edit(&cp)
			if got := MatchPattern(pattern, cp); got != test.want {
				t.Fatalf("match=%v", got)
			}
		})
	}
	for _, class := range []Class{Process, Destructive, Never} {
		p.Policy.Class = class
		if _, err := NewArgvPattern(p); err == nil {
			t.Fatal("offered", class)
		}
	}
}

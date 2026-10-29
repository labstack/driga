package command

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsCommandReadAndRefusal(t *testing.T) {
	s := runnerScope(t)
	if err := os.WriteFile(filepath.Join(s.Project, "read.txt"), []byte("native read"), 0600); err != nil {
		t.Fatal(err)
	}
	p := prepareRun(t, Request{Argv: []string{"type", "read.txt"}, Cwd: s.Project}, s)
	var out bytes.Buffer
	r, err := Run(context.Background(), p, &out, &bytes.Buffer{})
	if p.Policy.Class != Read || err != nil || r.ExitCode != 0 || out.String() != "native read" {
		t.Fatal(p.Policy, r, err, out.String())
	}
	for _, argv := range [][]string{{"type", "/p", "read.txt"}, {"type", "*.txt"}, {"type", "read.txt:stream"}, {"cmd", "/c", "type read.txt"}, {"echo", "hello"}} {
		p = prepareRun(t, Request{Argv: argv, Cwd: s.Project}, s)
		if p.Policy.Class == Read {
			t.Fatalf("unsupported read: %v", argv)
		}
		_, err = Run(context.Background(), p, &out, &bytes.Buffer{})
		if err == nil || err.Error() != "Running commands needs a sandbox, which Windows does not have yet." {
			t.Fatal(argv, err)
		}
	}
	// An outside file cannot inherit the native read or a similarity grant.
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	p = prepareRun(t, Request{Argv: []string{"type", outside}, Cwd: s.Project}, s)
	if p.Policy.Class != Other || !p.Policy.RequiresCard {
		t.Fatal(p.Policy)
	}
	if _, err := NewArgvPattern(p); err == nil {
		t.Fatal("outside read gained a grant")
	}
	link := filepath.Join(s.Project, "alias.txt")
	if err := os.Link(outside, link); err != nil {
		t.Fatal(err)
	}
	p = prepareRun(t, Request{Argv: []string{"type", "alias.txt"}, Cwd: s.Project}, s)
	out.Reset()
	if _, err := Run(context.Background(), p, &out, &bytes.Buffer{}); err == nil || out.Len() != 0 {
		t.Fatal("hard link read", err, out.String())
	}

}

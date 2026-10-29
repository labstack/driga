package command

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseCommandRequest(t *testing.T) {
	for _, in := range []Request{{}, {Argv: []string{"ls"}, Script: "ls"}, {Argv: []string{"ls"}, TimeoutSeconds: -1}, {Argv: []string{"ls"}, TimeoutSeconds: 601}, {Argv: []string{""}}} {
		if _, err := Parse(in); err == nil {
			t.Fatalf("accepted %#v", in)
		}
	}
	in := Request{Argv: []string{"cat", "a; echo evil", "$(touch x)", ""}}
	p, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Commands[0], in.Argv) || p.Request.TimeoutSeconds != 120 {
		t.Fatalf("literal argv/default lost: %#v", p)
	}
	in.TimeoutSeconds = 600
	if p, err = Parse(in); err != nil || p.Request.TimeoutSeconds != 600 {
		t.Fatalf("600: %v %#v", err, p)
	}
}

func policyFor(t *testing.T, script string, s Scope) Policy {
	t.Helper()
	p, err := Parse(Request{Script: script, Cwd: s.Project})
	if err != nil {
		t.Fatal(err)
	}
	pol, err := Classify(p, s)
	if err != nil {
		t.Fatal(err)
	}
	return pol
}

func TestCommandReadPolicy(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	for _, name := range []string{"file", "file with spaces"} {
		if err := os.WriteFile(filepath.Join(s.Project, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, script := range []string{"ls -la -- .", "cat 'file with spaces'", "head -n 2 file", "tail -n 3 file", "wc -l file", "ps -ef", "pgrep -f pattern", "lsof -n -P", "du -sh .", "df -h", "uptime", "grep -i -- needle file", "find . -type f -name '*.go'", "git status --short", "git log -n 3", "git diff --stat", "git show HEAD", "git branch --list", "stat file", "which cat", "cat file | grep word | wc -l"} {
		if got := policyFor(t, script, s); got.Class != Read {
			if len(got.Commands) > 0 && got.Commands[0].SHA256 == ([32]byte{}) {
				t.Logf("trusted executable unavailable: %s", script)
				continue
			}
			t.Errorf("%s: %#v", script, got)
		}
	}
	for _, script := range []string{"ls --unknown", "cat $(echo file)", "cat < file", "ls > file", "eval ls", "find . -exec echo x \\;", "find . -delete", "find . -ok echo x \\;", "ls; cat file", "ls &", "X=x ls", "cat *.txt", "git -c core.pager=evil log", "git diff --ext-diff", "git show --textconv", "git log --output=file", "rg --pre evil word", "tail -f file"} {
		if got := policyFor(t, script, s); got.Class == Read {
			t.Errorf("unsafe Read: %s", script)
		}
	}
}

func TestCommandPolicyPrecedence(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	cases := map[string]Class{"kill 23": Process, "pkill -f dummy": Process, "killall dummy": Process, "taskkill /PID 23": Process, "Stop-Process -Id 23": Process, "rm " + s.Scratch + "/x": Other, "rm file": Destructive, "git push": Destructive, "git reset --hard": Destructive, "git clean -f": Destructive, "git branch -D x": Destructive, "git checkout -- .": Destructive, "docker rm x": Destructive, "docker rmi x": Destructive, "docker volume rm x": Destructive, "sudo ls": Never, "su user": Never, "doas ls": Never, "pkexec ls": Never, "systemctl restart x": Never, "launchctl unload x": Never, "chmod 777 /denied/x": Never, "chown user /denied/x": Never, "git branch --list": Read, "go test ./...": Other}
	s.Denied = []string{"/denied"}
	for script, want := range cases {
		if got := policyFor(t, script, s); got.Class != want {
			if want == Read && len(got.Commands) > 0 && got.Commands[0].SHA256 == ([32]byte{}) {
				continue
			}
			t.Errorf("%s: got %s want %s", script, got.Class, want)
		}
	}
	p, _ := Parse(Request{Argv: []string{"ls"}, Cwd: s.Project, Network: true})
	pol, err := Classify(p, s)
	if err != nil || pol.Class != Destructive {
		t.Fatalf("network: %#v %v", pol, err)
	}
}

func TestCommandExecutableAndPaths(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir(), Denied: []string{t.TempDir()}}
	t.Setenv("DRIGA_CONFIG", filepath.Join(s.Project, "driga-control.toml"))
	if got := policyFor(t, "cat driga-control.toml", s); got.Class != Never {
		t.Errorf("Driga config override: %s", got.Class)
	}
	fake := filepath.Join(s.Project, "ls")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := policyFor(t, "./ls", s); got.Class == Read {
		t.Fatal("project executable gained Read")
	}
	protectedExe := filepath.Join(s.Denied[0], "helper")
	if e := os.WriteFile(protectedExe, []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if got := policyFor(t, quotePolicyTest(protectedExe), s); got.Class != Never {
		t.Fatalf("protected executable: %s", got.Class)
	}
	for _, script := range []string{"touch /outside/file", "cp source /outside/file", "chmod 600 /outside/file", "eval 'rm file'", "sh -c 'rm file'", "env rm file"} {
		if got := policyFor(t, script, s); got.Class != Destructive {
			t.Errorf("hidden/outside effect %s: %s", script, got.Class)
		}
	}
	if err := os.Symlink(fake, filepath.Join(s.Project, "alias")); err == nil {
		if got := policyFor(t, "./alias", s); got.Class == Read {
			t.Fatal("alias gained Read")
		}
	}
	for _, script := range []string{"env ls", "sh -c ls", "command ls", "git -c alias.x='!evil' x", "cat " + s.Denied[0] + "/secret"} {
		if got := policyFor(t, script, s); got.Class == Read {
			t.Errorf("unsafe Read %s", script)
		}
	}
	link := filepath.Join(s.Project, "escape")
	if err := os.Symlink(s.Denied[0], link); err == nil {
		if got := policyFor(t, "cat escape/secret", s); got.Class != Never {
			t.Fatalf("protected symlink: %#v", got)
		}
	}
	p, _ := Parse(Request{Argv: []string{"git", "log"}, Cwd: s.Project})
	pol, err := Classify(p, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(pol.Commands) > 0 && pol.Commands[0].SHA256 == ([32]byte{}) {
		t.Log("trusted git unavailable on this platform")
		return
	}
	if pol.Class != Read || len(pol.Commands) == 0 || pol.Commands[0].Path == "git" {
		t.Fatalf("unresolved git: %#v", pol)
	}
	if !reflect.DeepEqual(p.Commands[0], []string{"git", "log"}) {
		t.Fatal("classification mutated request")
	}
	a := Prepared{Parsed: p, Scope: s, Policy: pol}
	b := a
	b.Scope.Project = s.Scratch
	if Digest(a) == Digest(b) {
		t.Fatal("scope not bound")
	}
}

func quotePolicyTest(s string) string { return "'" + s + "'" }

func TestGitGlobalOptionsRequireApproval(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	for _, script := range []string{
		"git --no-pager reset --hard", "git -C dir push", "git -Cdir push",
		"git --git-dir=.git --work-tree=. clean -f", "git --no-optional-locks branch -D topic",
		"git -c k=v reset --hard", "git -ck=v push", "git --config-env=k=VALUE reset --hard",
		"git --unknown reset --hard", "git -C", "git -c", "git --git-dir",
		"git -c core.fsmonitor=evil status", "git -c alias.x='!rm -rf x' x",
	} {
		t.Run(script, func(t *testing.T) {
			if got := policyFor(t, script, s); got.Class != Destructive {
				t.Fatalf("got %s, want approval", got.Class)
			}
		})
	}
}

func TestExecutingWrappersRequireApproval(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	cases := [][]string{
		{"find", ".", "-exec", "rm", "-rf", "x", ";"},
		{"find", ".", "-execdir", "rm", "-rf", "x", "+"},
		{"find", ".", "-ok", "rm", "x", ";"}, {"find", ".", "-okdir", "rm", "x", ";"},
		{"find", ".", "-delete"}, {"find", ".", "-fprint", "x"},
		{"xargs", "rm", "-rf", "x"}, {"xargs"}, {"env", "rm", "x"},
		{"nice", "-n", "5", "rm", "x"}, {"timeout", "5", "rm", "x"}, {"nohup", "rm", "x"},
		{"sh", "-c", "rm x"}, {"bash", "-c", "rm x"}, {"zsh", "-c", "rm x"},
		{"sudo", "rm", "x"}, {"doas", "rm", "x"}, {"su", "-c", "rm x"}, {"pkexec", "rm", "x"},
		{"env", "sudo", "rm", "x"}, {"nice", "env", "doas", "rm", "x"},
		{"git", "-c", "alias.x=!rm -rf x", "x"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			p, err := Parse(Request{Argv: args, Cwd: s.Project})
			if err != nil {
				t.Fatal(err)
			}
			got, err := Classify(p, s)
			if err != nil {
				t.Fatal(err)
			}
			want := Destructive
			if slices.Contains([]string{"sudo", "doas", "su", "pkexec"}, args[0]) || slices.Contains(args, "sudo") || slices.Contains(args, "doas") {
				want = Never
			}
			if got.Class != want {
				t.Fatalf("got %s, want %s", got.Class, want)
			}
		})
	}
}

func TestCommandRunnersFailClosed(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	for _, tc := range []struct {
		argv []string
		want Class
	}{
		{[]string{"flock", ".lock", "/bin/rm", "-rf", "victim"}, Destructive},
		{[]string{"unbuffer", "rm", "victim"}, Destructive},
		{[]string{"script", "-c", "rm victim", "typescript"}, Destructive},
		{[]string{"watch", "rm victim"}, Destructive},
		{[]string{"parallel", "rm", "{}", ":::", "victim"}, Destructive},
		{[]string{"stdbuf", "-oL", "rm", "victim"}, Destructive},
		{[]string{"time", "rm", "victim"}, Destructive},
		{[]string{"timeout", "5", "rm", "victim"}, Destructive},
		{[]string{"nice", "-n", "5", "rm", "victim"}, Destructive},
		{[]string{"nohup", "rm", "victim"}, Destructive},
		{[]string{"env", "X=y", "rm", "victim"}, Destructive},
		{[]string{"command", "rm", "victim"}, Destructive},
		{[]string{"builtin", "exec", "rm", "victim"}, Destructive},
		{[]string{"exec", "rm", "victim"}, Destructive},
		{[]string{"xargs", "rm", "victim"}, Destructive},
		{[]string{"sh", "-c", "rm victim"}, Destructive},
		{[]string{"bash", "-c", "rm victim"}, Destructive},
		{[]string{"zsh", "-c", "rm victim"}, Destructive},
		{[]string{"dash", "-c", "rm victim"}, Destructive},
		{[]string{"fish", "-c", "rm victim"}, Destructive},
		{[]string{"sudo", "rm", "victim"}, Never},
		{[]string{"sudoedit", "victim"}, Never},
		{[]string{"su", "-c", "rm victim"}, Never},
		{[]string{"doas", "rm", "victim"}, Never},
		{[]string{"pkexec", "rm", "victim"}, Never},
		{[]string{"runuser", "-u", "root", "--", "rm", "victim"}, Never},
		{[]string{"chroot", "/", "rm", "victim"}, Never},
		{[]string{"gosu", "root", "rm", "victim"}, Never},
		// A benign or unparseable nested command still needs approval.
		{[]string{"flock", ".lock", "cat", "read.txt"}, Destructive},
		{[]string{"flock", "--unsupported", ".lock", "cat", "read.txt"}, Destructive},
		{[]string{"unbuffer"}, Destructive},
		{[]string{"script"}, Destructive},
		{[]string{"watch"}, Destructive},
		{[]string{"parallel"}, Destructive},
		{[]string{"sh", "-c", "$(cat command.txt)"}, Destructive},
		// Parsed nested commands must preserve the stronger Never policy.
		{[]string{"flock", ".lock", "sudo", "ls"}, Never},
		{[]string{"unbuffer", "-p", "sudo", "ls"}, Never},
		{[]string{"script", "-c", "sudo ls", "typescript"}, Never},
		{[]string{"script", "-q", "typescript", "sudo", "ls"}, Never},
		{[]string{"watch", "sudo ls"}, Never},
		{[]string{"parallel", "sudo", "ls", ":::", "victim"}, Never},
		{[]string{"stdbuf", "-oL", "sudo", "ls"}, Never},
		{[]string{"time", "sudo", "ls"}, Never},
		{[]string{"timeout", "5", "sudo", "ls"}, Never},
		{[]string{"nice", "-n", "5", "sudo", "ls"}, Never},
		{[]string{"nohup", "sudo", "ls"}, Never},
		{[]string{"env", "X=y", "sudo", "ls"}, Never},
		{[]string{"command", "sudo", "ls"}, Never},
		{[]string{"builtin", "exec", "sudo", "ls"}, Never},
		{[]string{"exec", "sudo", "ls"}, Never},
		{[]string{"xargs", "sudo", "ls"}, Never},
		{[]string{"sh", "-c", "sudo ls"}, Never},
		{[]string{"bash", "-c", "sudo ls"}, Never},
		{[]string{"zsh", "-c", "sudo ls"}, Never},
		{[]string{"dash", "-c", "sudo ls"}, Never},
		{[]string{"fish", "-c", "sudo ls"}, Never},
	} {
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			p, err := Parse(Request{Argv: tc.argv, Cwd: s.Project})
			if err != nil {
				t.Fatal(err)
			}
			got, err := Classify(p, s)
			if err != nil || got.Class != tc.want {
				t.Fatalf("got %s, %v; want %s", got.Class, err, tc.want)
			}
		})
	}
}

func TestPolicyEscapeAudit(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	for _, script := range []string{
		"rm -- -victim", "./rm victim", "/bin/rm victim",
		"mv file /outside/victim", "cp file /outside/victim",
		"eval 'rm victim'", "source script.sh", ". script.sh", "exec rm victim", "builtin eval 'rm victim'",
		"busybox rm victim", "toybox rm victim", "setsid rm victim", "stdbuf -oL rm victim", "time rm victim",
		"git custom-alias", "git --no-pager status", "git diff --ext-diff", "git show --textconv",
	} {
		t.Run(script, func(t *testing.T) {
			if got := policyFor(t, script, s); got.Class != Destructive {
				t.Fatalf("got %s, want approval", got.Class)
			}
		})
	}
	for _, script := range []string{
		"env sudo rm victim", "nohup doas rm victim", "timeout 5 pkexec rm victim",
		"sh -c 'sudo rm victim'", "bash -lc 'su -c evil'", "eval 'sudo rm victim'",
		"find . -exec sudo rm victim \\;", "runuser -u root -- rm victim", "sudoedit victim",
	} {
		t.Run(script, func(t *testing.T) {
			if got := policyFor(t, script, s); got.Class != Never {
				t.Fatalf("got %s, want refusal", got.Class)
			}
		})
	}
}

func TestAdditionalPolicyOptionEscapes(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	for _, script := range []string{
		"rg --pre evil needle", "rg --pre=evil needle", "rg --hostname-bin=evil needle",
		"cp --target-directory=/outside file", "mv -t/outside file",
		"RM.EXE victim", "ENV.EXE rm victim",
	} {
		t.Run(script, func(t *testing.T) {
			if got := policyFor(t, script, s); got.Class != Destructive {
				t.Fatalf("got %s, want approval", got.Class)
			}
		})
	}
}

func TestExplicitGitPathsRequireApproval(t *testing.T) {
	s := Scope{Project: t.TempDir(), Scratch: t.TempDir()}
	for _, script := range []string{"/usr/bin/git status", "./git status"} {
		if got := policyFor(t, script, s); got.Class != Destructive {
			t.Errorf("%s: got %s, want approval", script, got.Class)
		}
	}
}

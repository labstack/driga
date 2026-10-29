package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/labstack/driga/internal/kill"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
)

type Capability struct {
	Available                  bool
	Backend, Reason, OSVersion string
}
type Result struct {
	Output    string `json:"output,omitempty"`
	ExitCode  int
	TimedOut  bool
	Processes kill.SnapshotResult
}

// ProcessTarget includes live-only ownership explicitly in the prepared digest;
// model.Proc intentionally excludes those fields from its ordinary JSON format.
type ProcessTarget struct {
	Proc       model.Proc
	OwnerUID   uint32
	OwnerSID   string
	OwnerKnown bool
}
type ProcessSnapshot struct {
	Targets, Protected []ProcessTarget
	Force              bool
}

func ProbeSandbox(ctx context.Context) Capability { return nativeCapability(ctx) }

// Prepare binds canonical trusted scope, capability, generated profile, command
// identities and exact effective arguments. It grants no execution authority.
func Prepare(ctx context.Context, in Request, s Scope) (Prepared, error) {
	for _, key := range []string{"DRIGA_DATA_DIR", "DRIGA_CONFIG", "DRIGA_SPOOL_DIR"} {
		if value := os.Getenv(key); value != "" {
			s.Denied = append(s.Denied, value)
		}
	}
	s, err := normalizeScope(s)
	if err != nil {
		return Prepared{}, err
	}
	for _, root := range []string{s.Project, s.Scratch} {
		if root == "" {
			continue
		}
		st, e := os.Stat(root)
		if e != nil || !st.IsDir() {
			return Prepared{}, fmt.Errorf("scope root must be an existing directory: %s", root)
		}
		home, _ := os.UserHomeDir()
		if root == filepath.Dir(root) || root == home || denied(root, s) {
			return Prepared{}, fmt.Errorf("unsafe scope root: %s", root)
		}
	}
	if s.Scratch == "" {
		return Prepared{}, fmt.Errorf("private scratch directory required")
	}
	if in.Cwd == "" {
		in.Cwd = s.Project
		if in.Cwd == "" {
			in.Cwd = s.Scratch
		}
	}
	in.Cwd, err = canonical(in.Cwd)
	if err != nil {
		return Prepared{}, err
	}
	if (!within(in.Cwd, s.Project) && !within(in.Cwd, s.Scratch)) || denied(in.Cwd, s) {
		return Prepared{}, fmt.Errorf("cwd is outside the selected scope")
	}
	parsed, err := Parse(in)
	if err != nil {
		return Prepared{}, err
	}
	pol, err := Classify(parsed, s)
	if err != nil {
		return Prepared{}, err
	}
	p := Prepared{Parsed: parsed, Scope: s, Policy: pol, Sandbox: ProbeSandbox(ctx)}
	if p.Sandbox.Available {
		writes, err := sandboxWrites(s)
		if err != nil {
			return Prepared{}, err
		}
		p.WritableRoots = writes.Roots
		p.BackendExecutable = resolve(p.Sandbox.Backend, in.Cwd)
		p.Profile, err = sandboxProfile(p)
		if err != nil {
			return Prepared{}, err
		}
	}
	p.seal = Digest(p)
	return p, nil
}

func validatePrepared(p Prepared) error {
	if p.seal == ([32]byte{}) || p.seal != Digest(p) {
		return fmt.Errorf("prepared command changed")
	}
	parsed, err := Parse(p.Parsed.Request)
	if err != nil {
		return err
	}
	pol, err := Classify(parsed, p.Scope)
	if err != nil {
		return err
	}
	// Reparse, reclassify and resolve identities instead of trusting fields from a
	// transport. Changing a symlink, executable or protected root invalidates it.
	if !reflect.DeepEqual(parsed, p.Parsed) || !reflect.DeepEqual(pol, p.Policy) {
		return fmt.Errorf("command policy or executable changed")
	}
	s, err := normalizeScope(p.Scope)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(s, p.Scope) {
		return fmt.Errorf("scope changed")
	}
	cwd, err := canonical(p.Parsed.Request.Cwd)
	if err != nil || cwd != p.Parsed.Request.Cwd {
		return fmt.Errorf("cwd changed")
	}
	if p.Policy.Class == Never {
		return fmt.Errorf("%s", p.Policy.Reason)
	}
	return nil
}

func safeEnvironment(s Scope) []string {
	return []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=" + filepath.Join(s.Scratch, "home"), "TMPDIR=" + filepath.Join(s.Scratch, "tmp"), "TMP=" + filepath.Join(s.Scratch, "tmp"), "LANG=C", "LC_ALL=C", "TERM=dumb", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "PAGER=cat", "GIT_ATTR_NOSYSTEM=1"}
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

// EffectiveArgv discloses the invocation built from a prepared request. It
// supplies presentation data only; Run still validates and consumes no authority.
func EffectiveArgv(p Prepared) ([]string, error) { return effectiveArgv(p) }

func effectiveArgv(p Prepared) ([]string, error) {
	if p.Policy.Class == Read {
		var stages []string
		for _, e := range p.Policy.Commands {
			a := []string{quote(e.Path)}
			for _, v := range e.Argv[1:] {
				a = append(a, quote(v))
			}
			stages = append(stages, strings.Join(a, " "))
		}
		if len(stages) == 1 {
			e := p.Policy.Commands[0]
			return append([]string{e.Path}, e.Argv[1:]...), nil
		}
		// These scripts consist solely of quoted, already validated stages.
		return []string{"/bin/sh", "-c", strings.Join(stages, " | ")}, nil
	}
	if p.Parsed.Request.Script != "" {
		return []string{"/bin/sh", "-c", p.Parsed.Request.Script}, nil
	}
	if len(p.Policy.Commands) != 1 {
		return nil, fmt.Errorf("missing effective command")
	}
	e := p.Policy.Commands[0]
	if !filepath.IsAbs(e.Path) || e.SHA256 == ([32]byte{}) {
		return nil, fmt.Errorf("executable unavailable: %s", e.Path)
	}
	return append([]string{e.Path}, e.Argv[1:]...), nil
}

func Run(ctx context.Context, p Prepared, stdout, stderr io.Writer) (Result, error) {
	result := Result{ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := validatePrepared(p); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.Parsed.Request.TimeoutSeconds)*time.Second)
	defer cancel()
	if p.Policy.Class == Process {
		if p.ProcessSnapshot == nil || len(p.ProcessSnapshot.Targets) == 0 {
			return result, fmt.Errorf("process execution requires a separately prepared target snapshot")
		}
		var targets, protected []model.Proc
		for _, t := range p.ProcessSnapshot.Targets {
			targets = append(targets, targetProc(t))
		}
		for _, t := range p.ProcessSnapshot.Protected {
			protected = append(protected, targetProc(t))
		}
		r, e := kill.Snapshot(ctx, platform.New(), targets, protected, p.ProcessSnapshot.Force, time.Second)
		return Result{ExitCode: 0, Processes: r, TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}, e
	}
	if _, e := processSelection(p.Parsed); e == nil {
		return result, fmt.Errorf("process commands cannot bypass snapshot execution")
	}
	if !p.Sandbox.Available {
		if p.Policy.Class != Read {
			return result, fmt.Errorf("%s", p.Sandbox.Reason)
		}
		return runSafeRead(ctx, p, stdout)
	}
	cap := ProbeSandbox(ctx)
	profile, profileErr := sandboxProfile(p)
	if profileErr != nil || cap != p.Sandbox || profile != p.Profile || !reflect.DeepEqual(resolve(cap.Backend, p.Parsed.Request.Cwd), p.BackendExecutable) {
		return result, fmt.Errorf("sandbox capability or profile changed")
	}
	for _, d := range []string{p.Scope.Scratch, filepath.Join(p.Scope.Scratch, "home"), filepath.Join(p.Scope.Scratch, "tmp")} {
		if err := os.MkdirAll(d, 0700); err != nil {
			return result, err
		}
		st, e := os.Lstat(d)
		if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return result, fmt.Errorf("scratch must be a private directory")
		}
		if e = os.Chmod(d, 0700); e != nil {
			return result, e
		}
	}
	argv, err := effectiveArgv(p)
	if err != nil {
		return result, err
	}
	cmd, cleanup, err := sandboxCommand(ctx, p, argv)
	if err != nil {
		return result, err
	}
	defer cleanup()
	cmd.Dir = p.Parsed.Request.Cwd
	cmd.Env = safeEnvironment(p.Scope)
	cmd.Stdin = nil
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = platform.RunChild(cmd)
	result.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return result, nil
	}
	return result, err
}

// When no kernel sandbox is available, a deliberately narrow cat form reads
// through os.Root, without executing any program. Refuse traversal-capable
// tools, Git helpers, devices, recursive reads and scopes containing secrets.
func runSafeRead(ctx context.Context, p Prepared, out io.Writer) (Result, error) {
	bad := func() (Result, error) {
		return Result{ExitCode: -1}, fmt.Errorf("this read requires a working sandbox")
	}
	if len(p.Policy.Commands) != 1 {
		return bad()
	}
	e := p.Policy.Commands[0]
	if filepath.Base(e.Path) != "cat" || len(e.Argv) < 2 {
		return bad()
	}
	for _, a := range e.Argv[1:] {
		if strings.HasPrefix(a, "-") {
			return bad()
		}
		path := a
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.Parsed.Request.Cwd, path)
		}
		q, err := canonical(path)
		if err != nil {
			return Result{}, err
		}
		root := p.Scope.Project
		if !within(q, root) {
			root = p.Scope.Scratch
		}
		if !within(q, root) || denied(q, p.Scope) {
			return bad()
		}
		for _, d := range p.Scope.Denied {
			if within(d, root) {
				return bad()
			}
		}
		r, err := os.OpenRoot(root)
		if err != nil {
			return Result{}, err
		}
		rel, _ := filepath.Rel(root, path)
		f, err := r.Open(rel)
		r.Close()
		if err != nil {
			return Result{}, err
		}
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			f.Close()
			return bad()
		}
		// Reject hard links too: an alias must not expose a protected inode.
		v := reflect.Indirect(reflect.ValueOf(st.Sys()))
		if v.IsValid() && v.Kind() == reflect.Struct {
			n := v.FieldByName("Nlink")
			if n.IsValid() && n.CanUint() && n.Uint() > 1 {
				f.Close()
				return bad()
			}
		}
		_, err = io.Copy(out, contextReader{ctx: ctx, r: f})
		f.Close()
		if err != nil {
			return Result{}, err
		}
	}
	return Result{ExitCode: 0}, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}

func probeBackend(ctx context.Context, c Capability) Capability {
	if c.Backend == "" {
		return c
	}
	dir, err := os.MkdirTemp("", "driga-sandbox-probe-")
	if err != nil {
		c.Reason = err.Error()
		return c
	}
	defer os.RemoveAll(dir)
	s, err := normalizeScope(Scope{Scratch: dir})
	if err != nil {
		c.Reason = err.Error()
		return c
	}
	p := Prepared{Scope: s, Parsed: Parsed{Request: Request{Cwd: dir}}, Sandbox: c}
	cmd, cleanup, err := sandboxCommand(ctx, p, []string{"/usr/bin/true"})
	if err != nil {
		c.Reason = err.Error()
		return c
	}
	defer cleanup()
	cmd.Dir = dir
	cmd.Env = safeEnvironment(s)
	if err = platform.RunChild(cmd); err != nil {
		c.Reason = "Sandbox backend is unusable: " + err.Error()
		return c
	}
	c.Available = true
	return c
}

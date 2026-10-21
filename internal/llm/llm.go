// Package llm is the only launcher for Driga's opt-in Claude calls.
package llm

import (
	"bytes"
	"context"
	"fmt"
	"github.com/labstack/driga/internal/secfile"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/driga/internal/platform"
)

type Claude struct {
	path, version string
	flags         map[string]bool
}

var flagPattern = regexp.MustCompile(`(?:^|[\s,\[(])(--[A-Za-z][A-Za-z0-9-]*|-[A-Za-z])(?:$|[\s,=\])<])`)

func internalEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "DRIGA_INTERNAL=") {
			env = append(env, v)
		}
	}
	return append(env, "DRIGA_INTERNAL=1")
}

// Probe pins the executable and reads its version and help once per invocation.
func Probe(ctx context.Context) (Claude, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return Claude{}, fmt.Errorf("find claude: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	probe := func(arg string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, path, arg)
		cmd.Env = internalEnv()
		// Node can exit before a piped stdout drains, truncating --help;
		// a regular file receives everything.
		f, err := secfile.CreateTemp("", "driga-claude-probe-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		defer f.Close()
		cmd.Stdout, cmd.Stderr = f, f
		runErr := platform.RunChild(cmd)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		output, err := io.ReadAll(io.LimitReader(f, 1<<20))
		if err != nil {
			return nil, err
		}
		if runErr != nil {
			return nil, fmt.Errorf("claude %s: %w: %s", arg, runErr, strings.TrimSpace(string(output)))
		}
		return output, nil
	}
	version, err := probe("--version")
	if err != nil {
		return Claude{}, err
	}
	help, err := probe("--help")
	if err != nil {
		return Claude{}, err
	}
	c := Claude{path: path, version: strings.TrimSpace(string(version)), flags: make(map[string]bool)}
	// Match each token independently so adjacent flags are not skipped.
	for _, token := range strings.Fields(string(help)) {
		for _, m := range flagPattern.FindAllStringSubmatch(" "+token+" ", -1) {
			c.flags[strings.TrimLeft(m[1], "-")] = true
		}
	}
	return c, nil
}

func (c Claude) Require(flags ...string) error {
	for _, flag := range flags {
		flag = strings.TrimLeft(flag, "-")
		if !c.flags[flag] {
			return fmt.Errorf("claude %s lacks --%s", c.version, flag)
		}
	}
	return nil
}

func (c Claude) Run(ctx context.Context, args []string, stdin string) ([]byte, error) {
	var out bytes.Buffer
	if err := c.RunTo(ctx, args, stdin, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// RunTo delivers stdout as it arrives, while retaining stderr for failures.
func (c Claude) RunTo(ctx context.Context, args []string, stdin string, stdout io.Writer) error {
	return c.RunIn(ctx, "", args, stdin, stdout)
}

// RunIn shares probing, process lifetime and internal-agent tagging with ask and summaries.
func (c Claude) RunIn(ctx context.Context, dir string, args []string, stdin string, stdout io.Writer) error {
	args = append([]string(nil), args...)
	named := false
	for i, arg := range args {
		if arg == "--name" {
			if named || i+1 == len(args) || args[i+1] != "driga-internal" {
				return fmt.Errorf("claude requires --name driga-internal")
			}
			named = true
		}
		if strings.HasPrefix(arg, "--name=") {
			return fmt.Errorf("claude requires --name driga-internal")
		}
	}
	if !named {
		args = append(args, "--name", "driga-internal")
	}
	if err := c.Require("name"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.path, args...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = internalEnv()
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = stdout
	stderr := limitedBuffer{limit: 64 << 10}
	cmd.Stderr = &stderr
	if err := platform.RunChild(cmd); err != nil {
		return fmt.Errorf("claude: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > b.limit-b.Len() {
		p = p[:b.limit-b.Len()]
	}
	_, err := b.Buffer.Write(p)
	return n, err
}

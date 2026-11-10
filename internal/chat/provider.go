package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/labstack/driga/internal/llm"
)

// providers resolves lazily for new conversations. Run routes only the persisted
// provider: a failed or interrupted turn must never be replayed elsewhere.
type providers struct {
	preference string
	probe      func(context.Context, string) (Agent, error)
}

func NewProvider(dataDir, preference string) Agent {
	if preference == "" {
		preference = "auto"
	}
	return &providers{preference: preference, probe: func(ctx context.Context, name string) (Agent, error) {
		switch name {
		case "claude":
			a := &Claude{DataDir: dataDir}
			_, err := a.probe(ctx)
			return a, err
		case "codex", "copilot":
			path, err := exec.LookPath(name)
			if err != nil {
				return nil, err
			}
			args := []string{"--help"}
			if name == "codex" {
				args = []string{"exec", "--help"}
			}
			help, err := providerMetadata(ctx, path, args)
			if err != nil {
				return nil, err
			}
			if name == "codex" {
				if err := codexVersion(ctx, path); err != nil {
					return nil, err
				}
				for _, flag := range []string{"--ignore-user-config", "--ignore-rules", "--json", "--sandbox", "--strict-config"} {
					if !strings.Contains(help, flag) {
						return nil, fmt.Errorf("codex lacks %s; upgrade Codex CLI", flag)
					}
				}
				return &Codex{Path: path, DataDir: dataDir}, nil
			}
			// Headless/stdio are hidden SDK flags. Run verifies their protocol
			// handshake before sending a prompt; probe the visible isolation flags.
			for _, flag := range []string{"--disable-builtin-mcps", "--no-custom-instructions", "--no-bash-env", "--no-auto-update"} {
				if !strings.Contains(help, flag) {
					return nil, fmt.Errorf("copilot lacks %s; upgrade Copilot CLI", flag)
				}
			}
			return &Copilot{Path: path, DataDir: dataDir}, nil
		default:
			return nil, fmt.Errorf("unknown assistant provider %q", name)
		}
	}}
}

func (p *providers) Select(ctx context.Context) (string, error) {
	return p.SelectProvider(ctx, p.preference)
}

func validProviderPreference(preference string) bool {
	return preference == "auto" || preference == "claude" || preference == "codex" || preference == "copilot"
}

// SelectProvider resolves a new conversation's explicit preference without
// changing the broker default or any existing conversation's pinned provider.
func (p *providers) SelectProvider(ctx context.Context, preference string) (string, error) {
	if !validProviderPreference(preference) {
		return "", errors.New("assistant provider must be auto, claude, codex or copilot")
	}
	names := []string{preference}
	if preference == "auto" {
		names = []string{"claude", "codex", "copilot"}
	}
	var failures []error
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := p.probe(ctx, name); err == nil {
			return name, nil
		} else {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
		}
	}
	return "", fmt.Errorf("no compatible assistant available; install and sign in to Claude Code, Codex CLI or Copilot CLI: %w", errors.Join(failures...))
}

func (p *providers) Run(ctx context.Context, r Request, emit func(Delta) error) error {
	if r.Provider == "" || r.Provider == "auto" {
		return errors.New("conversation has no pinned assistant provider; start a new conversation")
	}
	a, err := p.probe(ctx, r.Provider)
	if err != nil {
		return fmt.Errorf("conversation requires %s; restore that CLI or start a new conversation: %w", r.Provider, err)
	}
	return a.Run(ctx, r, emit)
}

type metadataBuffer struct{ data []byte }

func (b *metadataBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 64<<10 {
		return 0, errors.New("assistant metadata exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
func providerMetadata(ctx context.Context, path string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out metadataBuffer
	err := llm.RunProvider(ctx, path, "", args, nil, nil, &out, io.Discard)
	return string(out.data), err
}

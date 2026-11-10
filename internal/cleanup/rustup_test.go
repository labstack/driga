package cleanup

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/driga/internal/model"
)

func TestRustupReviewsIndividualToolchainsAgainstKnownRepositories(t *testing.T) {
	e, _, _ := cacheFixture(t)
	t.Setenv("RUSTUP_HOME", "")
	t.Setenv("RUSTUP_TOOLCHAIN", "")
	home := filepath.Join(e.Home, ".rustup")
	for _, name := range []string{"stable-aarch64-apple-darwin", "nightly-2026-10-01-aarch64-apple-darwin", "1.70.0-aarch64-apple-darwin"} {
		put(t, filepath.Join(home, "toolchains", name, "lib", "fixture"), "toolchain")
	}
	e.Tool = func(_ context.Context, binary string, args ...string) ([]byte, error) {
		if binary == "rustup" && strings.Join(args, " ") == "toolchain list" {
			return []byte("stable-aarch64-apple-darwin (default)\nnightly-2026-10-01-aarch64-apple-darwin\n1.70.0-aarch64-apple-darwin\n"), nil
		}
		return nil, exec.ErrNotFound
	}
	repo := t.TempDir()
	put(t, filepath.Join(repo, "rust-toolchain.toml"), "[toolchain]\nchannel = 'nightly-2026-10-01'\n")
	s := snapshot{repos: []model.Repo{{Path: repo}}}
	rows := cacheCandidates(context.Background(), e, s)
	count := 0
	for _, row := range rows {
		if !strings.HasPrefix(row.ID, "cache:rustup:") {
			continue
		}
		count++
		reason := strings.Join(row.Evidence, "; ")
		unused := strings.HasPrefix(filepath.Base(row.Path), "1.70.0-")
		if row.Status != "report_only" || unused != strings.Contains(reason, "not referenced by known repositories") {
			t.Fatalf("incorrect toolchain usage: %+v", row)
		}
		if _, err := Apply(context.Background(), e, row, ApplyOpts{}); err == nil {
			t.Fatal("report-only toolchain became removable")
		}
	}
	if count != 3 {
		t.Fatalf("individual toolchain reviews=%d, want 3", count)
	}
}

func TestRustupRepositorySelectionAndUnknowns(t *testing.T) {
	customPath := filepath.Join(t.TempDir(), "custom", "compiler")
	for _, tc := range []struct {
		name, file, text, want string
		invalid                bool
	}{
		{"legacy", "rust-toolchain", "nightly-2026-09-15\n", "nightly-2026-09-15", false},
		{"legacy TOML", "rust-toolchain", "[toolchain]\nchannel = 'beta'\n", "beta", false},
		{"custom path", "rust-toolchain.toml", fmt.Sprintf("[toolchain]\npath = %q\n", customPath), customPath, false},
		{"invalid", "rust-toolchain.toml", "not = [ valid", "", true},
		{"ambiguous", "rust-toolchain.toml", "[toolchain]\nchannel = 'stable'\npath = '/custom/compiler'\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			put(t, filepath.Join(root, tc.file), tc.text)
			child := filepath.Join(root, "nested", "checkout")
			got, err := rustupRepositoryPin(child)
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("pin=%q err=%v", got, err)
			}
		})
	}
	if !rustupPinMatches("1.85.2-x86_64-unknown-linux-gnu", "1.85") || rustupPinMatches("1.850-x86_64-unknown-linux-gnu", "1.85") {
		t.Fatal("minor version pin must match only that version family")
	}
}

func TestRustupBackgroundReviewNeverRunsTools(t *testing.T) {
	e, calls, _ := cacheFixture(t)
	t.Setenv("RUSTUP_HOME", "")
	put(t, filepath.Join(e.Home, ".rustup", "toolchains", "fixture"), "data")
	e.RunTools = false
	rows := rustupCandidates(context.Background(), e, snapshot{})
	if len(rows) != 1 || rows[0].ID != "cache:rustup" || rows[0].Status != "report_only" || len(*calls) != 0 {
		t.Fatalf("background inventory ran tools or claimed usage: %+v calls=%v", rows, *calls)
	}
}

func TestRustupUnknownUsageNeverClaimsUnused(t *testing.T) {
	e, _, _ := cacheFixture(t)
	t.Setenv("RUSTUP_HOME", "")
	t.Setenv("RUSTUP_TOOLCHAIN", "")
	put(t, filepath.Join(e.Home, ".rustup", "toolchains", "beta-fixture-host", "lib", "fixture"), "data")
	e.Tool = func(context.Context, string, ...string) ([]byte, error) { return []byte("beta-fixture-host\n"), nil }
	repo := t.TempDir()
	put(t, filepath.Join(repo, "rust-toolchain.toml"), "invalid = [ selection")
	rows := rustupCandidates(context.Background(), e, snapshot{repos: []model.Repo{{Path: repo}}})
	if len(rows) != 1 || !strings.Contains(strings.Join(rows[0].Evidence, ";"), "usage unavailable") {
		t.Fatalf("incomplete usage claimed toolchain unused: %+v", rows)
	}
	// A global selection is positive usage evidence even when another repo's
	// configuration is unreadable. It still never authorizes removal.
	put(t, filepath.Join(e.Home, ".rustup", "settings.toml"), "[overrides]\n'/another/repo' = 'beta'\n")
	rows = rustupCandidates(context.Background(), e, snapshot{repos: []model.Repo{{Path: repo}}})
	if len(rows) != 1 || rows[0].Status != "report_only" || !strings.Contains(strings.Join(rows[0].Evidence, ";"), "referenced by") {
		t.Fatalf("explicit rustup override was lost: %+v", rows)
	}
}

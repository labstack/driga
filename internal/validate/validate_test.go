package validate_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/labstack/driga/internal/adapter"
	"github.com/labstack/driga/internal/adapter/claude"
	"github.com/labstack/driga/internal/adapter/codex"
	"github.com/labstack/driga/internal/collector"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/derive"
	"github.com/labstack/driga/internal/fixturegen"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/signatures"
	"github.com/labstack/driga/internal/store"
	"github.com/labstack/driga/internal/view"
)

func TestHeldOutFixtures(t *testing.T) {
	const root = "../../testdata/fixtures"
	sets := map[string]string{}
	found := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && filepath.Base(filepath.Dir(path)) == "heldout" {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			sets[filepath.ToSlash(rel)] = path
			found[d.Name()] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"one-repo-one-agent", "many-repos-many-worktrees", "untested-versions", "linux-paths", "crash-partial-lines", "orphan-broker-tree", "real-second-machine"} {
		t.Run(required, func(t *testing.T) {
			if !found[required] {
				if required == "real-second-machine" {
					t.Skip("missing heldout set: real-second-machine")
				}
				t.Fatalf("missing heldout set: %s", required)
			}
		})
	}
	// Every heldout set is exercised, including future maintainer contributions.
	names := make([]string, 0, len(sets))
	for name := range sets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run("snapshot/"+name, func(t *testing.T) { validateFixture(t, sets[name]) })
	}
}

type sessionLabel struct {
	ExternalID string `json:"external_id"`
	Label      string `json:"label"`
}

type conflictLabel struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type expected struct {
	Sessions  []sessionLabel  `json:"sessions"`
	Orphans   []string        `json:"orphans"`
	Conflicts []conflictLabel `json:"conflicts"`
}

func readJSON(t *testing.T, path string, dst any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		t.Fatalf("%s: expected one JSON value, got %v", path, err)
	}
}

func restoredPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		if parts[i] == "git.fixture" {
			parts[i] = ".git"
		}
	}
	return filepath.FromSlash(strings.Join(parts, "/"))
}

func copyHome(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	var mtimes map[string]time.Time
	readJSON(t, filepath.Join(src, "mtimes.json"), &mtimes)
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, restoredPath(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported fixture file: %s", path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// JSON-escape the replacement inside JSON/JSONL, including partial lines.
		replacement := []byte(dst)
		if strings.HasSuffix(path, ".json") || strings.Contains(path, ".jsonl") {
			quoted, err := json.Marshal(dst)
			if err != nil {
				return err
			}
			replacement = quoted[1 : len(quoted)-1]
		}
		b = bytes.ReplaceAll(b, []byte(fixturegen.HomeToken), replacement)
		if err := os.WriteFile(target, b, 0600); err != nil {
			return err
		}
		if at, ok := mtimes[filepath.ToSlash(rel)]; ok {
			return os.Chtimes(target, at, at)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func validateFixture(t *testing.T, src string) {
	t.Helper()
	home := copyHome(t, src)
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("DRIGA_DATA_DIR", t.TempDir())
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, ".driga/events"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	// Prevent system/user Git configuration, optional locks, and inherited repo
	// variables from consulting the maintainer's checkout or changing fixtures.
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_COUNT"} {
		t.Setenv(key, "") // Registers restoration of the original value.
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	var sc fixturegen.Scenario
	readJSON(t, filepath.Join(home, "scenario.json"), &sc)
	if sc.Now.IsZero() {
		t.Fatal("fixture must declare fixed Now")
	}
	var want expected
	readJSON(t, filepath.Join(home, "expected.json"), &want)
	if want.Sessions == nil || want.Orphans == nil || want.Conflicts == nil {
		t.Fatal("expected.json must contain sessions, orphans, and conflicts arrays (use [] when empty)")
	}
	if filepath.Base(src) == "many-repos-many-worktrees" && (sc.Repos != 50 || sc.WorktreesPerRepo < 2) {
		t.Fatal("many-repos-many-worktrees must contain 50 repos and multiple worktrees per repo")
	}
	fake, err := platform.LoadFake(home)
	if err != nil {
		t.Fatal(err)
	}
	sigs, err := signatures.Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	// A temp HOME is intentionally classified as temporary by adapters. Explicit
	// roots let Git discovery exercise the copied corpus on macOS and Linux alike.
	cfg.Roots = []string{filepath.Join(home, sc.Layout)}
	st, err := collector.Snapshot(context.Background(), collector.Deps{
		Cfg: cfg, Plat: fake, Adapters: []adapter.Adapter{claude.New(), codex.New()}, Sigs: sigs,
		SpoolDir: config.SpoolDir(), Now: func() time.Time { return sc.Now },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sources, err := st.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.LastError != "" || !source.LastOKAt.Equal(sc.Now) {
			t.Errorf("fixture %s: source %s failed: %+v", src, source.Name, source)
		}
	}
	ss, err := st.Sessions(store.SessionFilter{IncludeEnded: true})
	if err != nil {
		t.Fatal(err)
	}
	got := expected{Sessions: []sessionLabel{}, Orphans: []string{}, Conflicts: []conflictLabel{}}
	for _, s := range ss {
		got.Sessions = append(got.Sessions, sessionLabel{s.ExternalID, derive.Display(s.Label, s.Heuristic)})
		if s.LastPrompt != "" {
			t.Errorf("fixture %s: privacy default stored prompt for %s", src, s.ExternalID)
		}
		for _, c := range s.Completions {
			if c.Text != "" {
				t.Errorf("fixture %s: privacy default stored completion text for %s", src, s.ExternalID)
			}
		}
	}
	oset, err := st.Orphans()
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range oset {
		got.Orphans = append(got.Orphans, o.ID)
	}
	if filepath.Base(src) == "orphan-broker-tree" {
		for _, o := range oset {
			if o.RootPID == 8100 && (o.ProcCount != 3 || o.RSSTotal != 35<<20 || o.Young) {
				t.Errorf("broker tree must include exactly 3 processes, 35 MiB, mature; got %+v (§7.2)", o)
			}
			if o.RootPID == 8200 && !o.Young {
				t.Errorf("two-minute helper must be young (§7.2): %+v", o)
			}
		}
	}
	cv, err := view.Conflicts(st, view.Filter{}, sc.Now)
	if err != nil {
		t.Fatal(err)
	}
	// Driga reports one canonical identity per path (Review Focus 3); compare
	// conflict paths in fixture-home-token form so /var and /private/var agree.
	tokenHome, _ := filepath.EvalSymlinks(home)
	tokenize := func(p string) string {
		if tokenHome != "" {
			p = strings.ReplaceAll(p, tokenHome, fixturegen.HomeToken)
		}
		return strings.ReplaceAll(p, home, fixturegen.HomeToken)
	}
	for _, c := range append(cv.Current, cv.Recent...) {
		got.Conflicts = append(got.Conflicts, conflictLabel{c.Kind, tokenize(c.Path)})
	}
	for i := range want.Conflicts {
		want.Conflicts[i].Path = tokenize(want.Conflicts[i].Path)
	}
	normalize := func(e *expected) {
		sort.Slice(e.Sessions, func(i, j int) bool { return e.Sessions[i].ExternalID < e.Sessions[j].ExternalID })
		sort.Strings(e.Orphans)
		sort.Slice(e.Conflicts, func(i, j int) bool {
			if e.Conflicts[i].Kind != e.Conflicts[j].Kind {
				return e.Conflicts[i].Kind < e.Conflicts[j].Kind
			}
			return e.Conflicts[i].Path < e.Conflicts[j].Path
		})
	}
	normalize(&want)
	normalize(&got)
	// Deliberately fail, with spec references, rather than fitting labels or
	// production thresholds to the result of this independent heldout run.
	for _, check := range []struct {
		name, spec string
		want, got  any
	}{
		{"sessions", "§7.1 table / §6.2 hosted threads", want.Sessions, got.Sessions},
		{"orphans", "§7.2", want.Orphans, got.Orphans},
	} {
		if diff := cmp.Diff(check.want, check.got); diff != "" {
			t.Errorf("fixture %s: %s mismatch (%s), -expected +got:\n%s", src, check.name, check.spec, diff)
		}
	}
	if diff := cmp.Diff(want.Conflicts, got.Conflicts); diff != "" {
		// Print every missing/extra kind+path without cmp's long-path elision.
		missing, extra := conflictDelta(want.Conflicts, got.Conflicts, home)
		t.Errorf("fixture %s: conflicts mismatch (§7.3 / §7.1a families): expected %d, got %d; missing=%v; unexpected=%v", src, len(want.Conflicts), len(got.Conflicts), missing, extra)
	}
	repos, err := st.Repos()
	if err != nil {
		t.Fatal(err)
	}
	ws, err := st.Worktrees(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != sc.Repos || len(ws) != sc.Repos*sc.WorktreesPerRepo {
		t.Errorf("fixture %s: repos/worktrees = %d/%d, expected %d/%d (§6.5)", src, len(repos), len(ws), sc.Repos, sc.Repos*sc.WorktreesPerRepo)
	}
	if filepath.Base(src) == "untested-versions" {
		for _, a := range sc.Agents {
			v, err := st.Meta("warn:" + a.Adapter + ":untested-version")
			if err != nil || v != a.Version {
				t.Errorf("untested %s version warning = %q, %v; expected %q (§2a)", a.Adapter, v, err, a.Version)
			}
		}
	}
	items, err := st.Inbox(time.Time{}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.AckedAt == nil {
			t.Errorf("first-import event %s must be baselined (§7.1b)", item.ID)
		}
	}
	entries, err := os.ReadDir(config.DataDir())
	if err != nil || len(entries) != 0 {
		t.Errorf("snapshot wrote persistent data: %v, %v", entries, err)
	}
}

func conflictDelta(want, got []conflictLabel, home string) (missing, extra []string) {
	counts := map[conflictLabel]int{}
	for _, c := range want {
		counts[c]++
	}
	for _, c := range got {
		counts[c]--
	}
	canonicalHome, _ := filepath.EvalSymlinks(home)
	reportPath := func(p string) string {
		if canonicalHome != "" {
			p = strings.ReplaceAll(p, canonicalHome, fixturegen.HomeToken)
		}
		return strings.ReplaceAll(p, home, fixturegen.HomeToken)
	}
	for c, n := range counts {
		for ; n > 0; n-- {
			missing = append(missing, c.Kind+":"+reportPath(c.Path))
		}
		for ; n < 0; n++ {
			extra = append(extra, c.Kind+":"+reportPath(c.Path))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

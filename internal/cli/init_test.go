package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/service"
	"github.com/pelletier/go-toml/v2"
)

func TestInitSuggestsOnlyDirsWithRepos(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{"code/r1/.git", "src", "dev/a/b/c/d/.git", "work/node_modules/pkg/.git"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, "git/a/b/c"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "git/a/b/c/.git"), []byte("gitdir: /elsewhere\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := SuggestRoots(home); !reflect.DeepEqual(got, []string{"~/code", "~/git"}) {
		t.Fatal(got)
	}
}

func initFixture(t *testing.T, input string) (*Globals, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	t.Setenv("DRIGA_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	out := new(bytes.Buffer)
	return &Globals{Stdin: strings.NewReader(input), Stdout: out, Stderr: out}, out
}

func TestConfigShowDefaults(t *testing.T) {
	g, out := initFixture(t, "")
	if err := (ConfigShowCmd{}).Run(g); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"roots = []", "root_depth = 3", "no_service_hint = false", "[adapters.claude]", "[adapters.codex]", "extra_signatures = []"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %s: %s", want, out)
		}
	}
	var values map[string]any
	if err := toml.Unmarshal(out.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
	if values["capture_text"] != "titles" || values["stalled_after"] != "10m" || values["orphan_min_age"] != "10m" {
		t.Fatal(values)
	}
}

func TestConfigShowValueSources(t *testing.T) {
	g, out := initFixture(t, "")
	if err := os.MkdirAll(filepath.Dir(config.Path()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.Path(), []byte("capture_text = 'titles'\n[summaries]\nmodel = 'custom'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRIGA_CAPTURE_TEXT", "snippets")
	if err := (ConfigShowCmd{}).Run(g); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# source: env\ncapture_text = 'snippets'", "# source: file\nmodel = 'custom'", "# source: default\nroot_depth = 3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	var values map[string]any
	if err := toml.Unmarshal(out.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
}

func TestInitAllSkipped(t *testing.T) {
	g, _ := initFixture(t, "\n\n\n\n\n")
	if err := (InitCmd{}).Run(g); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config.Path()); !os.IsNotExist(err) {
		t.Fatalf("skipped init wrote config: %v", err)
	}
}

func TestInitWritesPrivateCommentedConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	g, _ := initFixture(t, "y\ny\ny\nn\nn\n")
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), "code", "repo", ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := (InitCmd{}).Run(g); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CaptureText != "snippets" || len(cfg.Roots) != 1 {
		t.Fatal(cfg)
	}
	data, err := os.ReadFile(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# root_depth = 3") {
		t.Fatal(string(data))
	}
	for path, mode := range map[string]os.FileMode{config.Path(): 0600, filepath.Dir(config.Path()): 0700} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: %v %v", path, info, err)
		}
	}
}

func TestInitDeclinesOverwrite(t *testing.T) {
	g, out := initFixture(t, "n\ny\ny\nn\nn\nn\n")
	if err := os.MkdirAll(filepath.Dir(config.Path()), 0700); err != nil {
		t.Fatal(err)
	}
	before := []byte("root_depth = 7\n")
	if err := os.WriteFile(config.Path(), before, 0600); err != nil {
		t.Fatal(err)
	}
	if err := (InitCmd{}).Run(g); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(config.Path())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("%s %v", after, err)
	}
	if !strings.Contains(out.String(), "--- ") || !strings.Contains(out.String(), "Overwrite?") {
		t.Fatal(out.String())
	}
}

func TestConfigCommentedRoundTrip(t *testing.T) {
	initFixture(t, "")
	cfg := config.Default()
	cfg.Roots = []string{"/tmp/repository with spaces"}
	cfg.RootDepth = 4
	cfg.Exclude = []string{"**/vendor"}
	cfg.CaptureText = "snippets"
	cfg.NoServiceHint = true
	cfg.OpenCmd = "editor {cwd}"
	cfg.Adapters["claude"] = config.AdapterConfig{Enabled: false}
	cfg.ExtraSignatures = []string{"/tmp/custom.toml"}
	if err := writeConfig(config.Path(), configText(cfg, true, nil)); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("got %+v; want %+v", got, cfg)
	}
}

func TestConfigEditCreatesDefaults(t *testing.T) {
	g, out := initFixture(t, "")
	// Use this test binary as the editor; Windows need not have Unix cat.
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_WANT_CONFIG_EDITOR", "1")
	t.Setenv("EDITOR", "\""+filepath.ToSlash(bin)+"\" -test.run=^TestConfigEditorHelper$ --")
	if err := (ConfigEditCmd{}).Run(g); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "# capture_text") {
		t.Fatal(out.String())
	}
	cfg, err := config.Load(config.Path())
	if err != nil || !reflect.DeepEqual(cfg, config.Default()) {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestConfigEditorHelper(t *testing.T) {
	if os.Getenv("GO_WANT_CONFIG_EDITOR") != "1" {
		return
	}
	f, err := os.Open(os.Args[len(os.Args)-1])
	if err != nil {
		os.Exit(1)
	}
	_, err = io.Copy(os.Stdout, f)
	_ = f.Close()
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestTask18CommandParsing(t *testing.T) {
	for _, args := range [][]string{{"service", "install"}, {"service", "uninstall"}, {"service", "start"}, {"service", "stop"}, {"service", "restart"}, {"service", "status"}, {"service", "logs", "-f"}, {"doctor", "--json"}, {"init"}, {"config", "path"}, {"config", "show"}, {"config", "edit"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var root struct {
				Globals
				Service ServiceCmd `cmd:""`
				Doctor  DoctorCmd  `cmd:""`
				Init    InitCmd    `cmd:""`
				Config  ConfigCmd  `cmd:""`
			}
			parser, err := kong.New(&root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parser.Parse(args); err != nil {
				t.Fatal(err)
			}
			// Parsing only: never execute service control commands in tests.
		})
	}
}

func TestServiceLogsLast50(t *testing.T) {

	if runtime.GOOS == "linux" {
		t.Skip("linux reads collector logs from journalctl; see TestServiceLogsLinux")
	}
	g, out := initFixture(t, "")
	if err := os.MkdirAll(filepath.Dir(service.LogPath()), 0700); err != nil {
		t.Fatal(err)
	}
	var log, want strings.Builder
	for i := 0; i < 60; i++ {
		line := strings.Repeat("x", i+1) + "\n"
		log.WriteString(line)
		if i >= 10 {
			want.WriteString(line)
		}
	}
	if err := os.WriteFile(service.LogPath(), []byte(log.String()), 0600); err != nil {
		t.Fatal(err)
	}
	if err := (ServiceCmd{Action: "logs"}).Run(g); err != nil {
		t.Fatal(err)
	}
	if out.String() != want.String() {
		t.Fatalf("wrong log tail: %s", out)
	}
}

// init and config show rewrite the whole file from configText; fleet and
// intake settings (including the share_text privacy choice) must survive.
func TestConfigTextKeepsFleetAndIntake(t *testing.T) {
	readEnv(t)
	text := "[fleet]\nserve = true\nlisten = '127.0.0.1:9999'\nshare_text = 'none'\nenroll_window = '2m'\nallow_any_address = true\n[intake]\notlp = true\nlisten = '127.0.0.1:7777'\n"
	if err := os.WriteFile(config.Path(), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	want, err := config.Load(config.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, commented := range []bool{true, false} {
		if err := os.WriteFile(config.Path(), []byte(configText(want, commented, nil)), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := config.Load(config.Path())
		if err != nil {
			t.Fatalf("commented=%v: %v", commented, err)
		}
		if got.Fleet != want.Fleet || got.Intake != want.Intake {
			t.Fatalf("commented=%v: fleet %+v intake %+v, want %+v %+v", commented, got.Fleet, got.Intake, want.Fleet, want.Intake)
		}
	}
}

func TestConfigShowRejectsJSON(t *testing.T) {
	readEnv(t)
	var out, errout bytes.Buffer
	if code := run([]string{"config", "show", "--json"}, "test", nil, &out, &errout); code == 0 || !strings.Contains(errout.String(), "--json") {
		t.Fatal(code, out.String(), errout.String())
	}
}

// On a case-insensitive disk ~/Projects and ~/projects are one directory.
func TestSuggestRootsCaseInsensitiveOnce(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Projects", "r", ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "projects")); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	if got := SuggestRoots(home); !reflect.DeepEqual(got, []string{"~/Projects"}) {
		t.Fatal(got)
	}
}

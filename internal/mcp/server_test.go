package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/labstack/driga/internal/cli"
	drigamcp "github.com/labstack/driga/internal/mcp"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/platform"
	"github.com/labstack/driga/internal/store"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
)

type toolCase struct {
	name string
	args map[string]any
	cli  []string
}

func toolCases(id int64) []toolCase {
	return []toolCase{
		{"get_summary", nil, []string{"summary"}},
		{"get_activity", map[string]any{"machine": "local"}, nil}, // MCP only: the TUI and web show activity
		{"get_flow", map[string]any{"id": id, "machine": "local"}, []string{"flow", fmt.Sprint(id), "--machine", "local"}},
		{"list_agents", map[string]any{"all": true, "machine": "local", "project": "api"}, []string{"agents", "--all", "--machine", "local", "--project", "api"}},
		{"get_agent", map[string]any{"id": id, "machine": "local"}, []string{"agents", "show", fmt.Sprint(id), "--machine", "local"}},
		{"list_projects", map[string]any{"active": true, "at_risk": true}, []string{"projects", "--active", "--at-risk"}},
		{"inspect_project", map[string]any{"project": "api"}, []string{"projects", "show", "api"}},
		{"list_inbox", map[string]any{"since": "yesterday", "all": true, "project": "api", "machine": "local"}, []string{"inbox", "--since", "yesterday", "--all", "--project", "api", "--machine", "local"}},
		{"list_conflicts", map[string]any{"project": "api", "machine": "local"}, nil}, // MCP only: Flow and Projects show overlaps
		{"list_orphans", map[string]any{"machine": "local"}, []string{"orphans", "--machine", "local"}},
		{"get_machine", map[string]any{"machine": "local"}, []string{"machines", "show", "local"}},
		{"list_machines", nil, []string{"machines"}},
		{"inspect_setup", nil, []string{"doctor"}},
		{"list_cleanup_candidates", nil, []string{"cleanup"}},
	}
}

func fixture(t *testing.T) (*store.Store, int64) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	t.Setenv("DRIGA_DATA_DIR", filepath.Join(home, "data"))
	t.Setenv("DRIGA_SPOOL_DIR", filepath.Join(home, "spool"))
	t.Setenv("DRIGA_CONFIG", filepath.Join(home, "config.toml"))
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("no_service_hint = true\n[adapters.copilot]\nenabled = false\n[adapters.claude]\nenabled = false\n[adapters.codex]\nenabled = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(filepath.Join(home, "data", "driga.db"), store.ReadWrite)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	// Reads trust the database only while its collector lives; this test
	// process stands in for it.
	if err := os.WriteFile(filepath.Join(home, "data", "driga.lock"), []byte(fmt.Sprintf(`{"pid":%d,"kind":"manual","executable":"/opt/driga","version":"test"}`, os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	// Future observations keep derived ages at zero, so only observed_at
	// changes between the real CLI and MCP reads.
	future := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	var id int64
	err = s.Tx(context.Background(), func(tx *store.Tx) error {
		if err := tx.SetMeta("last_pass_at", future.Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if err := tx.UpsertMachine(model.Machine{Name: "laptop"}); err != nil {
			return err
		}
		repo, err := tx.UpsertRepo(model.Repo{Name: "api", Path: "/fixture/api"})
		if err != nil {
			return err
		}
		if _, err := tx.UpsertWorktree(model.Worktree{RepoID: repo, Path: "/fixture/api", Dirty: 1, UpdatedAt: future}); err != nil {
			return err
		}
		id, err = tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "session", Cwd: "/fixture/api", StartedAt: future, LastActivityAt: future}, RepoID: &repo, Label: "working"})
		if err != nil {
			return err
		}
		if err := tx.UpsertInboxItem(model.InboxItem{ID: "unread", SessionID: id, Kind: "finished", TS: future}); err != nil {
			return err
		}
		if err := tx.ReplaceOrphans([]model.Orphan{{ID: "orphan", StartTime: future}}); err != nil {
			return err
		}
		remote := tx.ForMachine("remote")
		if err := remote.UpsertMachine(model.Machine{Name: "vm", State: "stale"}); err != nil {
			return err
		}
		_, err = remote.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "peer-session", StartedAt: future, LastActivityAt: future}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, id
}

func connect(t *testing.T, g *cli.Globals) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	a, b := sdk.NewInMemoryTransports()
	ss, err := cli.NewMCPServer(g).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	if got := cs.InitializeResult().ProtocolVersion; got != "2026-07-28" {
		t.Fatalf("protocol = %s", got)
	}
	return cs
}

func globals() *cli.Globals {
	return &cli.Globals{Machine: "all", Version: "test", Stdout: io.Discard, Stderr: io.Discard, CleanupPlatform: &platform.Fake{}}
}

func call(t *testing.T, cs *sdk.ClientSession, tc toolCase) *sdk.CallToolResult {
	t.Helper()
	args := tc.args
	if args == nil {
		args = map[string]any{}
	}
	r, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tc.name, Arguments: args})
	if err != nil || r.IsError {
		t.Fatalf("%s: result=%+v err=%v", tc.name, r, err)
	}
	if len(r.Content) != 1 {
		t.Fatalf("%s: content=%+v", tc.name, r.Content)
	}
	text, ok := r.Content[0].(*sdk.TextContent)
	if !ok || text.Text == "" || strings.Contains(text.Text, "\n") {
		t.Fatalf("%s: expected one-line summary, got %+v", tc.name, r.Content)
	}
	return r
}

func cliJSON(t *testing.T, args []string) []byte {
	t.Helper()
	var out bytes.Buffer
	root := cli.Root{Globals: *globals()}
	root.JSON, root.Stdout = true, &out
	p, err := kong.New(&root, kong.Bind(&root.Globals))
	if err != nil {
		t.Fatal(err)
	}
	c, err := p.Parse(append(append([]string{}, args...), "--json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func canonical(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var x any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&x); err != nil {
		t.Fatal(err)
	}
	var normalize func(any)
	normalize = func(x any) {
		switch x := x.(type) {
		case map[string]any:
			delete(x, "observed_at")
			delete(x, "trend_since")
			if _, flow := x["window_minutes"]; flow {
				delete(x, "from_at")
			}
			delete(x, "truncated")
			for _, v := range x {
				normalize(v)
			}
		case []any:
			for _, v := range x {
				normalize(v)
			}
		}
	}
	normalize(x)
	b, err = json.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEveryToolMatchesCLIJSON(t *testing.T) {
	_, id := fixture(t)
	cs := connect(t, globals())
	for _, tc := range toolCases(id) {
		t.Run(tc.name, func(t *testing.T) {
			r := call(t, cs, tc)
			got := r.StructuredContent
			if tc.name == "inspect_setup" {
				// The existing doctor CLI emits a bare array; §10 requires
				// MCP diagnostics to carry the shared schema-2 envelope.
				got = got.(map[string]any)["data"]
			}
			if tc.cli == nil {
				return
			}
			want := canonical(t, json.RawMessage(cliJSON(t, tc.cli)))
			if actual := canonical(t, got); !bytes.Equal(actual, want) {
				t.Fatalf("MCP: %s\nCLI: %s", actual, want)
			}
		})
	}
}

func TestNoToolAcks(t *testing.T) {
	s, id := fixture(t)
	before, err := s.Inbox(time.Time{}, true)
	if err != nil || len(before) != 1 || before[0].AckedAt != nil {
		t.Fatalf("before=%+v err=%v", before, err)
	}
	cs := connect(t, globals())
	for _, tc := range toolCases(id) {
		call(t, cs, tc)
	}
	after, err := s.Inbox(time.Time{}, true)
	if err != nil || !bytes.Equal(canonical(t, before), canonical(t, after)) {
		t.Fatalf("inbox changed: %+v err=%v", after, err)
	}
	if _, err := os.Stat(os.Getenv("DRIGA_SPOOL_DIR")); !os.IsNotExist(err) {
		t.Fatalf("tools created spool: %v", err)
	}
}

func TestToolDescriptionsSayReadsNeverAck(t *testing.T) {
	_, id := fixture(t)
	cs := connect(t, globals())
	r, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, tc := range toolCases(id) {
		want[tc.name] = true
	}
	for _, tool := range r.Tools {
		if slices.Contains(drigamcp.DashboardTools, tool.Name) {
			continue // Panel tools return fragments, not read envelopes.
		}
		if !want[tool.Name] {
			t.Fatalf("unexpected tool %s", tool.Name)
		}
		delete(want, tool.Name)
		for _, text := range []string{"Reading never acknowledges", `mode: "snapshot"`, "no history"} {
			if !strings.Contains(tool.Description, text) {
				t.Errorf("%s missing %q: %s", tool.Name, text, tool.Description)
			}
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || tool.OutputSchema == nil {
			t.Errorf("%s missing read-only hint or typed output schema", tool.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing tools: %v", want)
	}
}

func TestSnapshotModeLabelled(t *testing.T) {
	fixture(t)
	g := globals()
	g.Snapshot = true
	cs := connect(t, g)
	for _, name := range []string{"get_summary", "list_agents", "list_inbox", "list_machines", "inspect_setup"} {
		r := call(t, cs, toolCase{name: name})
		e := r.StructuredContent.(map[string]any)
		if e["mode"] != "snapshot" || e["schema"] != float64(2) {
			t.Fatalf("%s: %+v", name, e)
		}
		if !strings.Contains(r.Content[0].(*sdk.TextContent).Text, "snapshot · no history") {
			t.Fatalf("%s: %+v", name, r.Content)
		}
	}
}

func TestScopesAndToolErrors(t *testing.T) {
	s, id := fixture(t)
	cs := connect(t, globals())
	for _, tc := range []struct {
		machine string
		rows    int
	}{{"all", 2}, {"local", 1}, {"vm", 1}, {"remote", 1}} {
		r := call(t, cs, toolCase{name: "list_agents", args: map[string]any{"machine": tc.machine}})
		rows := r.StructuredContent.(map[string]any)["data"].(map[string]any)["rows"].([]any)
		if len(rows) != tc.rows {
			t.Fatalf("%s: %d rows, want %d", tc.machine, len(rows), tc.rows)
		}
		if tc.machine == "vm" && rows[0].(map[string]any)["machine_id"] != "remote" {
			t.Fatalf("remote scope: %+v", rows)
		}
	}
	for _, tc := range []toolCase{
		{name: "list_agents", args: map[string]any{"machine": "missing"}},
		{name: "list_agents", args: map[string]any{"project": "missing"}},
		{name: "list_inbox", args: map[string]any{"since": "-1h"}},
		{name: "list_inbox", args: map[string]any{"since": "invalid"}},
		{name: "get_agent", args: map[string]any{}},
		{name: "get_agent", args: map[string]any{"id": "invalid"}},
		{name: "get_agent", args: map[string]any{"id": id, "machine": "vm"}},
		{name: "inspect_project", args: map[string]any{}},
		{name: "inspect_project", args: map[string]any{"project": ""}},
	} {
		r, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: tc.name, Arguments: tc.args})
		if err != nil || !r.IsError || r.StructuredContent != nil {
			t.Fatalf("%s %v: result=%+v err=%v", tc.name, tc.args, r, err)
		}
	}
	// An explicit local path must not select a peer's identically named repo.
	dir := t.TempDir()
	t.Chdir(dir)
	err := s.Tx(context.Background(), func(tx *store.Tx) error {
		if _, err := tx.UpsertRepo(model.Repo{Name: "dot", Path: dir}); err != nil {
			return err
		}
		_, err := tx.ForMachine("remote").UpsertRepo(model.Repo{Name: "dot", Path: dir})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r := call(t, cs, toolCase{name: "inspect_project", args: map[string]any{"project": "."}})
	if got := r.StructuredContent.(map[string]any)["data"].(map[string]any)["machine_id"]; got != s.MachineID() {
		t.Fatalf("dot resolved to %v", got)
	}
}

func TestMachinesUsesStaleCache(t *testing.T) {
	s, _ := fixture(t)
	if err := os.Remove(filepath.Join(os.Getenv("DRIGA_DATA_DIR"), "driga.lock")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta("last_pass_at", time.Now().Add(-time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	r := call(t, connect(t, globals()), toolCase{name: "list_machines"})
	e := r.StructuredContent.(map[string]any)
	if e["mode"] != "daemon" {
		t.Fatalf("mode=%v", e["mode"])
	}
	data := e["data"].(map[string]any)
	if len(data["rows"].([]any)) != 2 || data["warning"] != "this machine's Driga service is not running" {
		t.Fatalf("cached machines: %+v", data)
	}
}

func TestMCPStdio(t *testing.T) {
	fixture(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestMCPStdioProcess$")
	cmd.Env = append(os.Environ(), "DRIGA_TEST_MCP_PROCESS=1")
	cmd.Stderr = io.Discard
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "stdio-test", Version: "test"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call(t, cs, toolCase{name: "get_summary"})
	// A valid JSON frame exceeding four MiB must close the transport rather
	// than reach the tool handler and return a normal validation error.
	_, err = cs.CallTool(ctx, &sdk.CallToolParams{Name: "inspect_project", Arguments: map[string]any{"project": strings.Repeat("x", 4*1024*1024+1)}})
	if err == nil {
		t.Fatal("stdio accepted a frame larger than four MiB")
	}
}

func TestMCPStdioProcess(t *testing.T) {
	if os.Getenv("DRIGA_TEST_MCP_PROCESS") != "1" {
		return
	}
	os.Exit(cli.Main([]string{"mcp"}, "test"))
}

type rpcLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *rpcLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *rpcLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestStructuredContentPreservesIntegers(t *testing.T) {
	s, _ := fixture(t)
	if err := s.Tx(context.Background(), func(tx *store.Tx) error {
		return tx.InsertSample(model.MachineSample{TS: time.Now(), MemTotal: 9007199254740993})
	}); err != nil {
		t.Fatal(err)
	}
	a, b := sdk.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := cli.NewMCPServer(globals()).Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	var log rpcLog
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "precision-test", Version: "test"}, nil).Connect(ctx, &sdk.LoggingTransport{Transport: b, Writer: &log}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	call(t, cs, toolCase{name: "get_machine", args: map[string]any{"machine": "local"}})
	// Inspect wire JSON before the SDK client decodes numbers into float64.
	for _, line := range strings.Split(log.String(), "\n") {
		var response struct {
			Result struct {
				StructuredContent json.RawMessage `json:"structuredContent"`
			} `json:"result"`
		}
		if !strings.HasPrefix(line, "read: ") {
			continue
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "read: ")), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Result.StructuredContent) == 0 {
			continue
		}
		got := canonical(t, response.Result.StructuredContent)
		want := canonical(t, json.RawMessage(cliJSON(t, []string{"machines", "show"})))
		if !bytes.Equal(got, want) {
			t.Fatalf("structured integers differ from CLI\nMCP: %s\nCLI: %s", got, want)
		}
		return
	}
	t.Fatal("no structured result on wire")
}

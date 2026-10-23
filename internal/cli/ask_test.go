package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/driga/internal/chat"
	"github.com/labstack/driga/internal/config"
	"github.com/labstack/driga/internal/store"
)

const askInit = `{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"connected"}]}`

func TestAskArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission-bit contract; Windows uses ACLs")
	}

	readEnv(t)
	record := fakeClaude(t, claudeHelp, `
prev=''
for arg in "$@"; do
 if [ "$prev" = '--mcp-config' ]; then /bin/cp -p "$arg" "$CLAUDE_RECORD.mcp"; fi
 prev="$arg"
done
echo '`+askInit+`'
echo '{"type":"result","result":"answer","is_error":false}'`)
	readRun(t, "ask", "where", "is", "fixture?")
	data, err := os.ReadFile(record + ".args")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(args) != 16 {
		t.Fatalf("args=%q", args)
	}
	mcpPath := args[5]
	tools := make([]string, len(chat.ReadTools))
	for i, name := range chat.ReadTools {
		tools[i] = "mcp__driga__" + name
	}
	want := []string{"-p", "--setting-sources", "", "--strict-mcp-config", "--mcp-config", mcpPath, "--tools", "", "--allowedTools", strings.Join(tools, ","), "--name", "driga-internal", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args=%q want=%q", args, want)
	}
	if filepath.Dir(mcpPath) != filepath.Join(os.Getenv("DRIGA_DATA_DIR"), "tmp") {
		t.Fatal(mcpPath)
	}
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Fatal("config survived run", err)
	}
	info, err := os.Stat(record + ".mcp")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	content, err := os.ReadFile(record + ".mcp")
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{"mcpServers": map[string]any{"driga": map[string]any{"command": executable, "args": []any{"mcp", "--chat"}, "env": map[string]any{"DRIGA_CHAT_READ_ONLY": "1", "DRIGA_DATA_DIR": config.DataDir(), "DRIGA_CONFIG": config.Path(), "DRIGA_SPOOL_DIR": config.SpoolDir()}}}}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("MCP config=%s", content)
	}
	stdin, err := os.ReadFile(record + ".stdin")
	if err != nil || !strings.HasSuffix(string(stdin), "User question:\nwhere is fixture?") {
		t.Fatal(string(stdin), err)
	}
}

// streamWriter checks delivery before the fake subprocess is allowed to finish.
type streamWriter struct {
	sync.Mutex
	buf  bytes.Buffer
	seen chan struct{}
	once sync.Once
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	n, err := w.buf.Write(p)
	if strings.Contains(w.buf.String(), "first ") {
		w.once.Do(func() { close(w.seen) })
	}
	return n, err
}

func TestAskStreams(t *testing.T) {
	readEnv(t)
	record := fakeClaude(t, claudeHelp, `echo '`+askInit+`'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"first "}]}}'
while [ ! -f "$CLAUDE_RECORD.release" ]; do /bin/sleep 0.01; done
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"second"}]}}'
echo '{"type":"result","result":"first second","is_error":false}'`)
	out := &streamWriter{seen: make(chan struct{})}
	var errout bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- run([]string{"ask", "question"}, "test", nil, out, &errout) }()
	defer os.WriteFile(record+".release", nil, 0600)
	select {
	case <-out.seen:
	case code := <-done:
		t.Fatalf("exited before streaming: %d %s", code, errout.String())
	case <-time.After(5 * time.Second):
		os.WriteFile(record+".release", nil, 0600)
		<-done
		t.Fatal("stdout was buffered until exit")
	}
	if err := os.WriteFile(record+".release", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if code := <-done; code != 0 {
		t.Fatal(code, errout.String())
	}
	out.Lock()
	defer out.Unlock()
	if strings.TrimSpace(out.buf.String()) != "first second" {
		t.Fatal(out.buf.String())
	}
}

func TestAskJSON(t *testing.T) {
	readEnv(t)
	fakeClaude(t, claudeHelp, `echo '`+askInit+`'
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"answer"}]}}'
echo '{"type":"result","result":"answer","is_error":false,"num_turns":2}'`)
	out, _ := readRun(t, "ask", "question", "--json")
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(out, err)
	}
	for _, field := range []string{"schema", "observed_at", "mode", "freshness", "coverage_gaps", "data"} {
		if _, ok := envelope[field]; !ok {
			t.Fatal("missing", field, out)
		}
	}
	if string(envelope["mode"]) != `"snapshot"` {
		t.Fatal("envelope mode must preserve the existing contract", out)
	}
	var result map[string]any
	if err := json.Unmarshal(envelope["data"], &result); err != nil {
		t.Fatal(err)
	}
	if result["result"] != "answer" || result["type"] != "result" || result["num_turns"] != float64(2) {
		t.Fatal(out)
	}
	readDB(t, func(tx *store.Tx) error { return tx.SetSourceOK("procs", time.Now()) })
	out, _ = readRun(t, "ask", "question", "--json")
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if string(envelope["mode"]) != `"daemon"` || !bytes.Contains(envelope["freshness"], []byte("procs")) {
		t.Fatal(out)
	}
}

func TestAskMissingFlag(t *testing.T) {
	readEnv(t)
	record := fakeClaude(t, strings.ReplaceAll(claudeHelp, "--strict-mcp-config", ""), `exit 99`)
	var out, errout bytes.Buffer
	if code := run([]string{"ask", "question"}, "test", nil, &out, &errout); code == 0 || !strings.Contains(errout.String(), "claude 2.1.303 lacks --strict-mcp-config") {
		t.Fatal(code, errout.String())
	}
	if _, err := os.Stat(record + ".calls"); !os.IsNotExist(err) {
		t.Fatal("invoked claude", err)
	}
}

func TestAskBadStreamAndCleanup(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"non-json", `echo nonsense`, "bad claude output"},
		{"missing-result", `echo '` + askInit + `'`, "missing final result"},
		{"mcp-not-loaded", `echo '{"type":"system","subtype":"init","mcp_servers":[]}'`, "driga MCP server"},
		{"mcp-failed", `echo '{"type":"system","subtype":"init","mcp_servers":[{"name":"driga","status":"failed"}]}'`, "driga MCP server"},
		{"missing-init", `echo '{"type":"result","result":"answer"}'`, "driga MCP server"},
		{"error-result", `echo '` + askInit + `'; echo '{"type":"result","is_error":true,"result":"failed"}'`, "reports an error"},
		{"exit", `echo 'details' >&2; exit 7`, "details"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readEnv(t)
			fakeClaude(t, claudeHelp, tc.body)
			var out, errout bytes.Buffer
			if code := run([]string{"ask", "question", "--json"}, "test", nil, &out, &errout); code == 0 || !strings.Contains(errout.String(), tc.want) {
				t.Fatal(code, out.String(), errout.String())
			}
			if !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"error":`) {
				t.Fatal("invalid structured failure", out.String())
			}
			entries, err := os.ReadDir(filepath.Join(os.Getenv("DRIGA_DATA_DIR"), "tmp"))
			if err != nil || len(entries) != 0 {
				t.Fatal("temporary config left", entries, err)
			}
		})
	}
}

func TestAskPrintsCitationsAsSessionNumbers(t *testing.T) {
	readEnv(t)
	fakeClaude(t, claudeHelp, `
echo '`+askInit+`'
echo '{"type":"result","result":"Waiting: [[session:207]] and [[session:2]].","is_error":false}'`)
	out, _ := readRun(t, "ask", "what needs me?")
	if !strings.Contains(out, "Waiting: #207 and #2.") || strings.Contains(out, "[[") {
		t.Fatalf("out=%q", out)
	}
}

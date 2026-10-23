package web

import (
	"encoding/json"
	"github.com/labstack/driga/internal/chat"
	"os"
	"os/exec"
	"testing"
)

func TestChatDOM(t *testing.T) {
	runtime, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	js, err := assets.ReadFile("static/chat.js")
	if err != nil {
		t.Fatal(err)
	}
	harness, err := os.ReadFile("testdata/chat-dom.js")
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		"answer":   chat.MarkdownHTML("# Heading\n\nA **bold** and *italic* with `inline` and [[session:42]].\n\n- [x] done\n\n| A | B |\n| --- | --- |\n| a | b |\n\n<script>alert(1)</script>\n\n[bad](javascript:alert(1))"),
		"partial":  chat.MarkdownHTML("**Hel"),
		"complete": chat.MarkdownHTML("**Hello**"),
	}
	cases, err := json.Marshal(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	cmd := nodeScript(runtime, "const serverHTML="+string(cases)+";\n"+string(harness)+"\n"+string(js)+"\nrunChatTests().catch(e=>{console.error(e);process.exitCode=1;});")
	cmd.Env = []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Chat DOM: %v: %s", err, out)
	}
}

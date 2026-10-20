package web

import (
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
	cmd := exec.Command(runtime, "-e", string(harness)+"\n"+string(js)+"\nrunChatTests().catch(e=>{console.error(e);process.exitCode=1;});")
	cmd.Env = []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Chat DOM: %v: %s", err, out)
	}
}

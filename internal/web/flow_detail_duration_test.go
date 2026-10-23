package web

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestFlowDetailJavaScriptUsesSharedDuration(t *testing.T) {
	runtime, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	js, err := assets.ReadFile("static/flow-map.js")
	if err != nil {
		t.Fatal(err)
	}
	harness, err := os.ReadFile("testdata/flow-review.js")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(runtime, "-")
	cmd.Stdin = strings.NewReader(string(harness) + "\n" + string(js))
	cmd.Env = []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detail: %v: %s", err, out)
	}
}

package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/labstack/driga/internal/spool"
)

func TestNeedsYouHookMetadataAndCodexReportOnly(t *testing.T) {
	dir := hookTestEnv(t)
	for _, adapter := range []string{"claude", "codex"} {
		event := "Notification"
		if adapter == "codex" {
			event = "PermissionRequest"
		}
		var out bytes.Buffer
		err := (HookCmd{Adapter: adapter, Event: event}).Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s","notification_type":"permission_prompt","tool_name":"Bash","tool_input":{"command":"private command"},"message":"private question"}`), Stdout: &out})
		if err != nil || out.Len() != 0 {
			t.Fatalf("report-only output: %q %v", out.String(), err)
		}
	}
	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("missing report-only event: %+v %v", got, err)
	}
	for _, h := range got {
		b, _ := json.Marshal(h)
		var v map[string]any
		json.Unmarshal(b, &v)
		if v["tool"] != "Bash" || h.Message != "" || strings.Contains(string(b), "private") {
			t.Fatalf("policy/metadata: %s", b)
		}
		if h.Adapter == "claude" && v["notification_type"] != "permission_prompt" {
			t.Fatalf("notification_type lost: %s", b)
		}
	}
}

func TestCodexPermissionHookFastExit(t *testing.T) {
	dir := hookTestEnv(t)
	var out bytes.Buffer
	cpu := func() time.Duration {
		var u syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &u); err != nil {
			t.Fatal(err)
		}
		return time.Duration(u.Utime.Nano() + u.Stime.Nano())
	}
	costs := make([]time.Duration, 5)
	for i := range costs {
		start := cpu()
		if err := (HookCmd{Adapter: "codex", Event: "PermissionRequest"}).Run(&Globals{Stdin: strings.NewReader(`{"session_id":"s","tool_name":"Bash","tool_input":{"command":"private"}}`), Stdout: &out}); err != nil {
			t.Fatal(err)
		}
		costs[i] = cpu() - start
	}
	slices.Sort(costs)
	if !testing.Short() && !raceEnabled && costs[2] >= 20*time.Millisecond {
		t.Fatalf("Codex median hook CPU %s", costs[2])
	}
	got, _, err := spool.Drain(dir, nil)
	if err != nil || len(got) != 5 || out.Len() != 0 {
		t.Fatal(got, err, out.String())
	}
}

func TestCodexPermissionArbitraryToolInput(t *testing.T) {
	dir := hookTestEnv(t)
	if err := (HookCmd{Adapter: "codex", Event: "PermissionRequest"}).Run(&Globals{Stdin: strings.NewReader(`{"session_id":"mcp","tool_name":"mcp__server__tool","tool_input":["opaque",1]}`)}); err != nil {
		t.Fatal(err)
	}
	events, _, err := spool.Drain(dir, nil)
	if err != nil || len(events) != 1 || events[0].Tool != "mcp__server__tool" {
		t.Fatal(events, err)
	}
}

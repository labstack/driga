package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWatchJSONIsCompactNDJSON(t *testing.T) {
	if os.Getenv("DRIGA_JSON_WATCH_CHILD") == "1" {
		os.Exit(Main([]string{"summary", "--json", "--watch", "5ms"}, "test"))
	}
	readEnv(t)
	st := readDB(t, nil)
	_ = st
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestWatchJSONIsCompactNDJSON$")
	cmd.Env = append(os.Environ(), "DRIGA_JSON_WATCH_CHILD=1")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	lines := make(chan []byte, 3)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for i := 0; i < 3 && scanner.Scan(); i++ {
			lines <- bytes.Clone(scanner.Bytes())
		}
	}()
	for range 3 {
		select {
		case line := <-lines:
			if !json.Valid(line) || !bytes.Contains(line, []byte(`"schema":2`)) {
				t.Fatalf("invalid NDJSON line %s", line)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("watch did not emit three envelopes")
		}
	}
	// This is the child started by this test, never a collector or unrelated PID.
	if err = cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("watch exit: %v %s", err, &diagnostic)
	}
}

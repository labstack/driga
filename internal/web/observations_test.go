package web

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

func TestFlowObservedRendering(t *testing.T) {
	s, st, id, _ := fixture(t)
	at := time.Now().Add(-time.Second).UTC().Truncate(time.Millisecond)
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		child, err := tx.UpsertSession(model.Session{ParentID: &id, SessionObs: model.SessionObs{Adapter: "codex", ExternalID: "observed-child"}, Label: "working"})
		if err != nil {
			return err
		}
		if err = tx.Observe(child, at, 123, 0); err != nil {
			return err
		}
		return tx.Observe(id, at, 12288, 250)
	}); err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", fmt.Sprint("/flow.json?id=", id), s.token).Body.String()
	for _, want := range []string{`"last_growth_at":"` + at.Format(time.RFC3339Nano) + `"`, `"last_growth_bytes":12288`, `"last_growth_bytes":123`, `"from_at"`, `"cpu_ms_per_min"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	css, _ := assets.ReadFile("static/flow-map.css")
	js, _ := assets.ReadFile("static/flow-map.js")
	for _, want := range []string{".metro-log-band", ".metro-cpu", "prefers-reduced-motion"} {
		if !strings.Contains(string(css), want) {
			t.Error("missing", want)
		}
	}
	if !strings.Contains(string(js), "driga:flow-refresh") {
		t.Fatal("missing JSON refresh hook")
	}
}

func TestFlowObservedJavaScript(t *testing.T) {
	js, _ := assets.ReadFile("static/app.js")
	harness, err := os.ReadFile("testdata/observations.js")
	if err != nil {
		t.Fatal(err)
	}
	code := "eval(" + strconv.Quote(string(js)) + "); eval(" + strconv.Quote(string(harness)) + ");"
	var cmd *exec.Cmd
	if runtime, err := exec.LookPath("node"); err == nil {
		cmd = exec.Command(runtime, "-e", code)
	} else {
		t.Skip("JavaScript runner unavailable")
	}
	cmd.Env = []string{"PATH=" + t.TempDir(), "HOME=" + t.TempDir()}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript observations: %v\n%s", err, out)
	}
}

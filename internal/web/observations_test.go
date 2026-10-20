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
	body := request(s, "GET", fmt.Sprint("/flow?id=", id), s.token).Body.String()
	for _, want := range []string{`data-growth-at="` + at.Format(time.RFC3339Nano) + `"`, `data-growth-bytes="12288"`, `data-growth-bytes="123"`, `60 one-minute transcript growth buckets`, `data-observation-strip`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(body, "60 one-minute transcript growth buckets") < 3 {
		t.Fatal("lead/worker/tail missing sparklines")
	}
	css, _ := assets.ReadFile("static/app.css")
	js, _ := assets.ReadFile("static/app.js")
	for _, want := range []string{".flow-observed-pulse", ".flow-growth-ring", "prefers-reduced-motion"} {
		if !strings.Contains(string(css), want) {
			t.Errorf("missing CSS %s", want)
		}
	}
	if !strings.Contains(string(js), "refreshFlowObservations();") {
		t.Fatal("growth observer not connected to refresh")
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
	} else if runtime, err := exec.LookPath("osascript"); err == nil {
		cmd = exec.Command(runtime, "-l", "JavaScript", "-e", code)
	} else {
		t.Skip("JavaScript runner unavailable")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript observations: %v\n%s", err, out)
	}
}

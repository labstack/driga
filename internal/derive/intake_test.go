package derive

import (
	"github.com/labstack/driga/internal/model"
	"strings"
	"testing"
	"time"
)

func intakeInput() (StateInput, time.Time) {
	now := time.Unix(1792324800, 0)
	n := now.Add(-time.Minute)
	return StateInput{Obs: model.SessionObs{Adapter: "claude", PID: 10, RegistryStatus: "busy", LastActivityAt: now.Add(-20 * time.Minute)}, LastNotification: &n}, now
}
func TestToolDecisionUserClearsWaiting(t *testing.T) {
	in, now := intakeInput()
	d := now.Add(-time.Second)
	in.LastDecision = &d
	l, _, _ := Label(in, now, Thresholds{})
	if l == "waiting" {
		t.Fatal(l)
	}
	in.Obs.PID = 0
	l, _, _ = Label(in, now, Thresholds{})
	if l != "dead" {
		t.Fatal(l)
	}
}
func TestToolDecisionConfigNoChange(t *testing.T) {
	in, now := intakeInput()
	l, _, _ := Label(in, now, Thresholds{})
	if l != "waiting" {
		t.Fatal(l)
	}
}
func TestAPIErrorEvidence(t *testing.T) {
	in, now := intakeInput()
	in.LastNotification = nil
	in.LastAPIError = "API error 529, attempt 3"
	l, _, e := Label(in, now, Thresholds{})
	if l != "stalled" || !strings.Contains(e, "API error 529, attempt 3") {
		t.Fatal(l, e)
	}
	a := now.Add(-3 * time.Second)
	in.LastAPIRequest = &a
	l, _, e = Label(in, now, Thresholds{})
	if l != "working" || !strings.Contains(e, "API call 3s ago") {
		t.Fatal(l, e)
	}
}

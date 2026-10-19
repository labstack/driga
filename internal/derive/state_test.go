package derive

import (
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestLabel(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	claude := func(status string, age time.Duration) StateInput {
		return StateInput{Obs: model.SessionObs{Adapter: "claude", PID: 10, RegistryStatus: status, LastActivityAt: ago(age)}, Proc: &model.Proc{CPUPercent: 0.2}}
	}
	codex := func(open, hosted bool, age time.Duration) StateInput {
		return StateInput{Obs: model.SessionObs{Adapter: "codex", PID: 10, TurnOpen: open, Hosted: hosted, LastActivityAt: ago(age)}, Proc: &model.Proc{}}
	}
	gemini := func(open bool, age time.Duration) StateInput {
		return StateInput{Obs: model.SessionObs{Adapter: "gemini", PID: 10, TurnOpen: open, LastActivityAt: ago(age)}, Proc: &model.Proc{}}
	}
	notification, stop := ago(time.Minute), ago(2*time.Minute)
	tests := []struct {
		name, label, evidence string
		heuristic             bool
		in                    StateInput
	}{
		{name: "claude notification", label: "waiting", in: claude("busy", 5*time.Minute)},
		{name: "claude busy recent", label: "working", in: claude("busy", time.Minute)},
		{name: "claude busy quiet low cpu", label: "stalled", evidence: "CPU 0.2%", in: claude("busy", 14*time.Minute)},
		{name: "claude busy threshold", label: "stalled", in: claude("busy", 10*time.Minute)},
		{name: "claude busy quiet high cpu", label: "unknown", evidence: "CPU 5.0%", in: claude("busy", 14*time.Minute)},
		{name: "claude busy cpu boundary", label: "unknown", evidence: "CPU 1.0%", in: claude("busy", 14*time.Minute)},
		{name: "claude busy no sample", label: "stalled", evidence: "no process sample", in: claude("busy", 14*time.Minute)},
		{name: "claude stop", label: "finished", in: claude("idle", 5*time.Minute)},
		{name: "claude stop already seen", label: "idle", in: claude("idle", 5*time.Minute)},
		{name: "claude stop before prompt", label: "idle", in: claude("idle", 5*time.Minute)},
		{name: "claude idle", label: "idle", in: claude("idle", time.Hour)},
		{name: "claude shell", label: "idle", in: claude("shell", time.Hour)},
		{name: "claude dead", label: "dead", in: claude("idle", time.Hour)},
		{name: "claude dead busy", label: "dead", in: claude("busy", time.Second)},
		{name: "claude dead unseen stop", label: "finished", in: claude("busy", time.Second)},
		{name: "claude busy before stop", label: "working", in: claude("busy", time.Minute)},
		{name: "claude pid zero no registry", label: "unknown", in: StateInput{Obs: model.SessionObs{Adapter: "claude"}}},
		{name: "claude notification old", label: "idle", in: claude("idle", time.Second)},
		{name: "codex recent", label: "working", in: codex(true, false, time.Second)},
		{name: "codex quiet open", label: "stalled", in: codex(true, false, 14*time.Minute)},
		{name: "codex tool approval never waiting", label: "unknown", in: codex(true, false, 2*time.Minute)},
		{name: "codex idle", label: "idle", in: codex(false, false, time.Hour)},
		{name: "codex unseen completion", label: "finished", in: codex(false, false, time.Hour)},
		{name: "codex gone completed", label: "finished", in: codex(false, false, time.Hour)},
		{name: "codex gone open", label: "dead", in: codex(true, false, time.Hour)},
		{name: "codex gone recent", label: "dead", in: codex(true, false, time.Second)},
		{name: "hosted recent", label: "working", heuristic: true, in: codex(true, true, time.Second)},
		{name: "hosted completed", label: "idle", heuristic: true, in: codex(false, true, time.Hour)},
		{name: "hosted old completed", label: "idle", heuristic: true, in: codex(false, true, 48*time.Hour)},
		{name: "hosted open quiet", label: "unknown", heuristic: true, in: codex(true, true, time.Hour)},
		{name: "hosted minute boundary", label: "unknown", heuristic: true, in: codex(true, true, time.Minute)},
		{name: "gemini recent", label: "working", in: gemini(true, time.Second)},
		{name: "gemini open tool never waiting", label: "unknown", in: gemini(true, 2*time.Minute)},
		{name: "gemini quiet open", label: "stalled", in: gemini(true, 14*time.Minute)},
		{name: "gemini completed alive", label: "idle", in: gemini(false, time.Hour)},
		{name: "gemini gone open", label: "dead", in: gemini(true, time.Hour)},
		{name: "gemini gone completed", label: "finished", in: gemini(false, time.Hour)},
		{name: "unknown adapter", label: "unknown", in: StateInput{Obs: model.SessionObs{Adapter: "other"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.name {
			case "claude notification", "claude notification old":
				tt.in.LastNotification = &notification
			case "claude busy quiet high cpu":
				tt.in.Proc.CPUPercent = 5
			case "claude busy cpu boundary":
				tt.in.Proc.CPUPercent = 1
			case "claude busy no sample":
				tt.in.Proc = nil
			case "claude stop", "claude stop already seen", "claude stop before prompt", "claude dead unseen stop", "claude busy before stop":
				tt.in.LastStop, tt.in.LastUserPrompt, tt.in.Unseen = &stop, ago(time.Hour), true
				if tt.name == "claude stop already seen" {
					tt.in.Unseen = false
				}
				if tt.name == "claude stop before prompt" {
					tt.in.LastUserPrompt = now
				}
			case "codex unseen completion", "hosted completed":
				tt.in.Unseen = true
			}
			if strings.HasPrefix(tt.name, "claude dead") {
				tt.in.Obs.PID = 0
			}
			if strings.HasPrefix(tt.name, "codex gone") || strings.HasPrefix(tt.name, "gemini gone") {
				tt.in.Proc = nil
			}
			label, heuristic, evidence := Label(tt.in, now, Thresholds{StuckAfter: 10 * time.Minute})
			if label != tt.label || heuristic != tt.heuristic {
				t.Fatalf("Label = (%q, %v), want (%q, %v); %s", label, heuristic, tt.label, tt.heuristic, evidence)
			}
			if strings.TrimSpace(evidence) == "" || !strings.Contains(evidence, tt.evidence) {
				t.Fatalf("evidence = %q, want nonempty containing %q", evidence, tt.evidence)
			}
			if tt.name == "claude busy quiet low cpu" && !strings.Contains(evidence, "14m") {
				t.Fatalf("duration not rounded: %q", evidence)
			}
			wantDisplay := label
			if label == "stalled" {
				wantDisplay = "stalled?"
			}
			if heuristic {
				wantDisplay = "~" + wantDisplay
			}
			if got := Display(label, heuristic); got != wantDisplay {
				t.Fatalf("Display = %q, want %q", got, wantDisplay)
			}
		})
	}
}

func TestBusyQuietWithCPUIsUnknown(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, cpu := range []float64{1, 5} {
		in := StateInput{
			Obs:  model.SessionObs{Adapter: "claude", PID: 10, RegistryStatus: "busy", LastActivityAt: now.Add(-14 * time.Minute)},
			Proc: &model.Proc{CPUPercent: cpu},
		}
		label, heuristic, evidence := Label(in, now, Thresholds{StuckAfter: 10 * time.Minute})
		if label != "unknown" || heuristic || !strings.Contains(evidence, "14m") || !strings.Contains(evidence, "CPU") {
			t.Fatalf("CPU %v: got %q, heuristic=%v, evidence=%q", cpu, label, heuristic, evidence)
		}
	}
}

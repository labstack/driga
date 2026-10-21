package derive

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestMixerRetainsLabelInputs(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	in := StateInput{Obs: model.SessionObs{Adapter: "claude", PID: 42, RegistryStatus: "busy", LastActivityAt: now.Add(-15 * time.Minute)}, Proc: &model.Proc{PID: 42, CPUPercent: 8.4}}
	m := LabelWithEvidence(in, now, Thresholds{StalledAfter: 20 * time.Minute})
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Input          *StateInput `json:"input"`
		StalledAfterMS int64       `json:"stalled_after_ms"`
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Input == nil || !reflect.DeepEqual(*saved.Input, in) || saved.StalledAfterMS != (20*time.Minute).Milliseconds() {
		t.Fatalf("label inputs/threshold not retained: %s", raw)
	}
}

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
	recent := ago(time.Second)
	tests := []struct {
		name, label, evidence string
		heuristic             bool
		in                    StateInput
	}{
		{name: "claude notification", label: "waiting", in: claude("busy", 5*time.Minute)},
		{name: "claude otlp activity", label: "working", in: claude("idle", time.Hour)},
		{name: "claude api activity", label: "working", in: claude("idle", time.Hour)},
		{name: "agy alive", label: "unknown", in: StateInput{Obs: model.SessionObs{Adapter: "agy", PID: 10}, Proc: &model.Proc{}}},
		{name: "agy gone", label: "dead", in: StateInput{Obs: model.SessionObs{Adapter: "agy"}}},
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
			case "claude otlp activity":
				tt.in.LastToolResult = &recent
			case "claude api activity":
				tt.in.LastAPIRequest = &recent
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
			label, heuristic, evidence := Label(tt.in, now, Thresholds{StalledAfter: 10 * time.Minute})
			mixed := LabelWithEvidence(tt.in, now, Thresholds{StalledAfter: 10 * time.Minute})
			if mixed.Verdict != label || mixed.Heuristic != heuristic || mixed.Evidence != evidence || len(mixed.Rows) != 4 {
				t.Fatalf("mixer disagrees: %+v", mixed)
			}
			agree := 0
			for _, row := range mixed.Rows {
				if row.Strength < 0 || row.Strength > 1 {
					t.Fatal(row)
				}
				if row.Supports {
					agree++
				}
			}
			if mixed.Agreement != agree {
				t.Fatal(mixed)
			}
			if !heuristic && mixed.Agreement < 1 {
				t.Errorf("non-heuristic rule has no support: %+v", mixed)
			}
			if !strings.HasPrefix(evidence, "because ") || !strings.Contains(evidence, ": ") {
				t.Errorf("missing deciding signal: %q", evidence)
			}

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

func TestMixerFinishedSupportsStopBehindLatestNotification(t *testing.T) {
	now := time.Now()
	stop, notification := now.Add(-2*time.Minute), now.Add(-time.Minute)
	m := LabelWithEvidence(StateInput{Obs: model.SessionObs{Adapter: "claude", PID: 10, RegistryStatus: "shell", LastActivityAt: now}, Proc: &model.Proc{}, LastStop: &stop, LastNotification: &notification, LastUserPrompt: now.Add(-time.Hour), Unseen: true}, now, Thresholds{})
	if m.Verdict != "finished" || !m.Rows[0].Supports || !m.Rows[1].Supports || !m.Rows[3].Supports || !strings.Contains(m.Evidence, "because hooks: unacknowledged Stop") {
		t.Fatal(m)
	}
	if m.Rows[1].Reading != "Notification" {
		t.Fatal("latest reading lost", m.Rows[1])
	}
}

func TestBusyQuietWithCPUIsUnknown(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, cpu := range []float64{1, 5} {
		in := StateInput{
			Obs:  model.SessionObs{Adapter: "claude", PID: 10, RegistryStatus: "busy", LastActivityAt: now.Add(-14 * time.Minute)},
			Proc: &model.Proc{CPUPercent: cpu},
		}
		label, heuristic, evidence := Label(in, now, Thresholds{StalledAfter: 10 * time.Minute})
		if label != "unknown" || heuristic || !strings.Contains(evidence, "14m") || !strings.Contains(evidence, "CPU") {
			t.Fatalf("CPU %v: got %q, heuristic=%v, evidence=%q", cpu, label, heuristic, evidence)
		}
	}
}

func TestMixerStateInputsProperty(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, adapter := range []string{"claude", "codex", "gemini", "agy", "other"} {
		for _, status := range []string{"", "busy", "idle", "shell"} {
			for _, age := range []time.Duration{0, time.Minute, 10 * time.Minute, 24 * time.Hour} {
				for _, cpu := range []float64{-1, 0, 0.99, 1, 5} {
					for _, hosted := range []bool{false, true} {
						for _, open := range []bool{false, true} {
							in := StateInput{Obs: model.SessionObs{Adapter: adapter, PID: 10, RegistryStatus: status, Hosted: hosted, TurnOpen: open, LastActivityAt: now.Add(-age)}}
							if cpu >= 0 {
								in.Proc = &model.Proc{CPUPercent: cpu}
							}
							for _, at := range []time.Time{{}, now.Add(-2 * time.Minute), now} {
								if !at.IsZero() {
									in.LastStop = &at
									in.LastAPIRequest = &at
									in.Unseen = true
								}
								m := LabelWithEvidence(in, now, Thresholds{})
								l, h, e := Label(in, now, Thresholds{})
								if m.Verdict != l || m.Heuristic != h || m.Evidence != e || len(m.Rows) != 4 {
									t.Fatal(in, m)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestMixerMissingAndConflictingSignals(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	missing := LabelWithEvidence(StateInput{Obs: model.SessionObs{Adapter: "claude"}}, now, Thresholds{})
	for _, r := range missing.Rows {
		if r.Strength != 0 || r.Supports && r.Name != "registry" {
			t.Fatal("missing signal gained support", r)
		}
	}
	stop, tool := now.Add(-time.Hour), now.Add(-time.Second)
	mixed := LabelWithEvidence(StateInput{Obs: model.SessionObs{Adapter: "claude", PID: 1, RegistryStatus: "idle", LastActivityAt: stop}, Proc: &model.Proc{CPUPercent: 18}, LastStop: &stop, LastToolResult: &tool, Unseen: true}, now, Thresholds{})
	if mixed.Verdict != "working" || mixed.Rows[0].Supports || mixed.Rows[1].Supports || !mixed.Rows[2].Supports || mixed.Rows[2].Age != time.Second || mixed.Rows[3].CPUPercent == nil || *mixed.Rows[3].CPUPercent != 18 {
		t.Fatal(mixed)
	}
}

func TestMixerMissingClaudeSampleIsNotProcessDeath(t *testing.T) {
	now := time.Now()
	m := LabelWithEvidence(StateInput{Obs: model.SessionObs{Adapter: "claude", PID: 1, RegistryStatus: "busy", LastActivityAt: now.Add(-time.Hour)}}, now, Thresholds{})
	if m.Verdict != "stalled" || m.Rows[3].Alive != nil || m.Rows[3].Strength != 0 {
		t.Fatal(m)
	}
}

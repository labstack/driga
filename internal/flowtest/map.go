package flowtest

import (
	"fmt"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
	"time"
)

// SeedMap is an eight-hour family with measured bursts and twelve quiet threads.
// Snapshot pruning leaves most of the four-hour zoom explicitly without data.
func SeedMap(tx *store.Tx, now time.Time, snapshot bool) (int64, error) {
	mixer := model.StateMixer{Verdict: "working", ObservedAt: now.Add(-5 * time.Second), Evidence: "stored hooks and process verdict", Agreement: 3, Rows: []model.StateSignal{{Name: "hooks", Reading: "tool result"}, {Name: "process", Reading: "alive · CPU 2.5%"}}}
	lead, err := tx.UpsertSession(model.Session{SessionObs: model.SessionObs{Adapter: "claude", ExternalID: "eight-hour-lead", PID: 100, Title: "Driga flow map", StartedAt: now.Add(-8 * time.Hour), LastActivityAt: now}, Label: "working", Mixer: &mixer})
	if err != nil {
		return 0, err
	}
	for i := 0; i < 480; i++ {
		at := now.Add(time.Duration(i-479) * time.Minute)
		if err := tx.Observe(lead, at, int64((i%7+1)*1000), int64(i%11*20)); err != nil {
			return 0, err
		}
		for j := 0; j < 4; j++ {
			if err := tx.InsertEvent(model.Event{Adapter: "claude", ExternalID: "eight-hour-lead", TS: at.Add(time.Duration(j)*time.Second - time.Minute), Kind: "otlp:tool_result", Metadata: &model.EventMetadata{Tool: "Read"}}); err != nil {
				return 0, err
			}
		}
	}
	for i := 0; i < 12; i++ {
		if _, err := tx.UpsertSession(model.Session{ParentID: &lead, SessionObs: model.SessionObs{Adapter: "codex", ExternalID: fmt.Sprintf("quiet-%02d", i), Hosted: true, Title: fmt.Sprintf("Quiet thread %02d", i+1), StartedAt: now.Add(time.Duration(i-420) * time.Minute)}, Label: "unknown"}); err != nil {
			return 0, err
		}
	}
	for i, adapter := range []string{"claude", "copilot", "codex", "gemini"} {
		mix := mixer
		mix.Verdict = []string{"working", "finished", "waiting", "unknown"}[i]
		mix.Evidence = "stored " + adapter + " verdict"
		s := model.Session{ParentID: &lead, SessionObs: model.SessionObs{Adapter: adapter, ExternalID: "map-" + adapter, Title: []string{"Implement geometry", "Review CSP", "Permission check", "Quiet remote analysis"}[i], StartedAt: now.Add(time.Duration(i-25) * time.Minute), LastActivityAt: now.Add(-time.Minute)}, Label: mix.Verdict, Mixer: &mix}
		if adapter == "copilot" {
			end := now.Add(-3 * time.Minute)
			s.EndedAt = &end
		}
		id, err := tx.UpsertSession(s)
		if err != nil {
			return 0, err
		}
		kind := []string{"otlp:tool_result", "agentStop", "PermissionRequest", "retained"}[i]
		if err := tx.InsertEvent(model.Event{Adapter: adapter, ExternalID: s.ExternalID, Kind: kind, TS: now.Add(-4 * time.Minute)}); err != nil {
			return 0, err
		}
		if adapter == "codex" {
			if err := tx.UpsertInboxItem(model.InboxItem{ID: "map-open", SessionID: id, Kind: "waiting", Reason: "permission", Tool: "Bash", TS: now.Add(-4 * time.Minute), WaitingSinceAt: now.Add(-5 * time.Minute), Jump: &model.InboxJump{Kind: "resume", Command: "codex resume 'map-codex'", Cwd: "/fixture/driga"}}); err != nil {
				return 0, err
			}
		}
		if adapter == "claude" {
			ack := now.Add(-8 * time.Minute)
			if err := tx.UpsertInboxItem(model.InboxItem{ID: "map-answered", SessionID: id, Kind: "waiting", Reason: "question", TS: now.Add(-12 * time.Minute), AckedAt: &ack}); err != nil {
				return 0, err
			}
			if err := tx.Observe(id, now.Add(-3*time.Minute), 5000, 250); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.InsertGap(model.Gap{From: now.Add(-18 * time.Minute), To: now.Add(-16 * time.Minute)}); err != nil {
		return 0, err
	}
	if snapshot {
		if err := tx.PruneSnapshot(now); err != nil {
			return 0, err
		}
	}
	return lead, nil
}

package store

import (
	"time"

	"github.com/labstack/driga/internal/model"
)

// LabelEvents returns the newest event of each exact kind for state labeling.
// Labeling consumes timestamps and API error summaries, not event metadata.
// Preserve LatestEvents' global order so cross-kind timestamp ties still use
// the last inserted event, including when fresh hooks are merged by callers.
func (q *querier) LabelEvents() ([]model.Event, error) {
	return readMany(q, `WITH newest AS (
		SELECT adapter,external_id,kind,MAX(ts) AS ts
		FROM events WHERE machine_id=? AND ts>=? GROUP BY adapter,external_id,kind
	), winners AS (
		SELECT MAX(e.id) AS id FROM newest n JOIN events e
		ON e.machine_id=? AND e.adapter=n.adapter AND e.external_id=n.external_id AND e.kind=n.kind AND e.ts=n.ts
		GROUP BY n.adapter,n.external_id,n.kind
	)
	SELECT adapter,external_id,ts,kind,CASE WHEN kind='otlp:api_error' THEN summary ELSE '' END
	FROM events WHERE id IN (SELECT id FROM winners) ORDER BY ts DESC,id DESC`, []any{q.machineID, unix(time.Time{}), q.machineID}, func(s scanner) (model.Event, error) {
		e := model.Event{MachineID: q.machineID}
		var ts int64
		err := s.Scan(&e.Adapter, &e.ExternalID, &ts, &e.Kind, &e.Summary)
		e.TS = fromUnix(ts)
		return e, err
	})
}

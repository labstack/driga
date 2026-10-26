package dashboard

import (
	"context"
	"encoding/json"
	"time"

	"github.com/labstack/driga/internal/panel"
)

// Live owns persistence followed by presentation. Errors mean nothing was
// saved; unavailable live readings become panel warnings carrying the committed
// identity and version. Callers must never retry a save to obtain live data.
type Live struct {
	store *Store
	run   func(context.Context, panel.RunRequest) (panel.Fragment, error)
}

func NewLive(store *Store, run func(context.Context, panel.RunRequest) (panel.Fragment, error)) *Live {
	return &Live{store: store, run: run}
}

func (l *Live) Open(ctx context.Context, id string, vars map[string]string) (panel.Fragment, error) {
	r, err := l.store.Get(id)
	return l.present(ctx, r, err, "Opened", vars)
}

// Refresh requires live results, so polling clients can retain their last good
// reading and mark it stale. Unlike a mutation, this operation can be retried.
func (l *Live) Refresh(ctx context.Context, id string, vars map[string]string) (panel.Fragment, error) {
	r, err := l.store.Get(id)
	if err != nil {
		return panel.Fragment{}, err
	}
	f, err := l.run(ctx, panel.RunRequest{Dashboard: r.Spec, Vars: vars})
	f.ID, f.Version = r.ID, r.Version
	return f, err
}

func (l *Live) Create(ctx context.Context, spec panel.Dashboard, author Author, message string, vars map[string]string) (panel.Fragment, error) {
	r, err := l.store.Create(spec, author, message)
	return l.present(ctx, r, err, "Created", vars)
}

func (l *Live) Edit(ctx context.Context, id string, ops []Operation, base int, author Author, message string, vars map[string]string) (panel.Fragment, error) {
	r, err := l.store.Edit(id, ops, base, author, message)
	return l.present(ctx, r, err, "Updated", vars)
}

func (l *Live) Replace(ctx context.Context, id string, spec panel.Dashboard, base int, author Author, message string, vars map[string]string) (panel.Fragment, error) {
	r, err := l.store.Replace(id, spec, base, author, message)
	return l.present(ctx, r, err, "Replaced", vars)
}

func (l *Live) Restore(ctx context.Context, id string, version, base int, author Author, vars map[string]string) (panel.Fragment, error) {
	r, err := l.store.Restore(id, version, base, author)
	return l.present(ctx, r, err, "Restored", vars)
}

func (l *Live) present(ctx context.Context, r Record, err error, verb string, vars map[string]string) (panel.Fragment, error) {
	if err != nil {
		return panel.Fragment{}, err
	}
	f, err := l.run(ctx, panel.RunRequest{Dashboard: r.Spec, Vars: vars})
	if err == nil {
		_, err = json.Marshal(f)
	}
	if err != nil {
		f = panel.Fragment{ObservedAt: time.Now().UTC().Format(time.RFC3339), Results: []panel.Result{}}
		for _, p := range r.Spec.Panels {
			f.Results = append(f.Results, panel.Result{ID: p.ID, Status: panel.StatusError, Message: verb + " dashboard; live data unavailable: " + err.Error()})
		}
	}
	f.Dashboard, f.ID, f.Version = r.Spec, r.ID, r.Version
	return f, nil
}

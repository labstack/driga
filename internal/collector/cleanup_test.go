package collector

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/feed"
	"github.com/labstack/driga/internal/model"
	"github.com/labstack/driga/internal/store"
)

type diskPlatform struct {
	*fakePlatform
	free, total uint64
}

func (p *diskPlatform) Machine(context.Context) (model.MachineSample, error) {
	return model.MachineSample{DiskFree: p.free, DiskTotal: p.total}, nil
}
func TestLowDiskInboxOnce(t *testing.T) {
	d, st, p, _ := setup(t)
	disk := &diskPlatform{fakePlatform: p, total: 1000}
	d.Plat = disk
	d.Adapters = nil
	d.Cfg.Machine.DiskFreeMin = 100
	for i, free := range []uint64{50, 50, 200, 50} {
		disk.free = free
		now := d.now().Add(time.Duration(i) * time.Second)
		d.Now = func() time.Time { return now }
		if i == 1 {
			d.State = NewState()
		}
		pass(t, d, st, Plan{Machine: true})
	}
	var lows []model.InboxItem
	for _, item := range inbox(t, st) {
		if item.Kind == "low_disk" {
			lows = append(lows, item)
		}
	}
	if len(lows) != 2 {
		t.Fatal(lows)
	}
	if err := st.Tx(context.Background(), func(tx *store.Tx) error { _, err := tx.AckInbox(lows[0].ID, d.now()); return err }); err != nil {
		t.Fatal(err)
	}
	pass(t, d, st, Plan{Machine: true})
	items, err := st.Inbox(time.Time{}, false)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range items {
		if item.Kind == "low_disk" {
			count++
		}
	}
	// The recovered episode is history; acknowledging the current episode
	// leaves no active warning, without creating a replacement until recovery.
	if count != 0 {
		t.Fatal(items)
	}
}

func TestLowDiskStaysLocal(t *testing.T) {
	d, st, p, _ := setup(t)
	d.Plat = &diskPlatform{fakePlatform: p, total: 1000, free: 1}
	d.Adapters = nil
	pass(t, d, st, Plan{Machine: true})
	if err := st.Tx(context.Background(), func(tx *store.Tx) error {
		rows, err := passFeedRows(tx, d.now(), feed.Titles)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.kind == feed.Inbox {
				t.Fatal("machine-only cleanup item leaked into session-referenced feed")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

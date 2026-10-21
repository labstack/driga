package store

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/driga/internal/model"
)

func TestWindowsWatchRoundTrip(t *testing.T) {
	s, err := Open("", Memory)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.Tx(context.Background(), func(tx *Tx) error {
		if err := tx.ReplaceProcs([]model.Proc{{PID: 42, Partial: true}}); err != nil {
			return err
		}
		return tx.InsertSample(model.MachineSample{TS: time.Now(), Load1: -1, Load5: -1, Load15: -1, CommitUsed: 10, CommitLimit: 20})
	})
	if err != nil {
		t.Fatal(err)
	}
	ps, err := s.Procs()
	if err != nil || len(ps) != 1 || !ps[0].Partial {
		t.Fatalf("%+v %v", ps, err)
	}
	m, err := s.LatestSample()
	if err != nil || m.Load1 != -1 || m.CommitUsed != 10 || m.CommitLimit != 20 {
		t.Fatalf("%+v %v", m, err)
	}
}

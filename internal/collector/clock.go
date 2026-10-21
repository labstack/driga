package collector

import "time"

// daemonClock lets scheduling tests advance collection and polling together.
type daemonClock interface {
	Now() time.Time
	NewTimer(time.Duration) daemonTimer
}

type daemonTimer interface {
	Chan() <-chan time.Time
	Stop()
}

type realDaemonClock struct{}

func (realDaemonClock) Now() time.Time { return time.Now() }
func (realDaemonClock) NewTimer(d time.Duration) daemonTimer {
	return realDaemonTimer{time.NewTimer(d)}
}

type realDaemonTimer struct{ *time.Timer }

func (t realDaemonTimer) Chan() <-chan time.Time { return t.C }
func (t realDaemonTimer) Stop()                  { t.Timer.Stop() }

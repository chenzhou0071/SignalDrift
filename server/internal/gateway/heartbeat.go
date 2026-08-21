// heartbeat.go — 心跳监督：定时巡逻，踢掉超过超时未心跳的连接（Sweep）
package gateway

import (
	"context"
	"time"
)

type HeartbeatWatcher struct {
	mgr        *SessionManager
	timeoutSec int64
	onTimeout  func(*Session)
	now        func() int64
}

func NewHeartbeatWatcher(mgr *SessionManager, timeoutSec int64, onTimeout func(*Session), now func() int64) *HeartbeatWatcher {
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}
	return &HeartbeatWatcher{mgr: mgr, timeoutSec: timeoutSec, onTimeout: onTimeout, now: now}
}

func (w *HeartbeatWatcher) Sweep() {
	n := w.now()
	w.mgr.Range(func(s *Session) bool {
		if n-s.LastBeat() > w.timeoutSec {
			w.onTimeout(s)
		}
		return true
	})
}

func (w *HeartbeatWatcher) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Sweep()
		}
	}
}

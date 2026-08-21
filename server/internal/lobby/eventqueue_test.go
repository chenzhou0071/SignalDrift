// eventqueue_test.go — 结算队列测试：结算全链路/Stop 幂等/Stop 后 Push 不 panic
package lobby

import (
	"testing"

	"signaldrift/server/internal/store"
)

func TestEventQueueMatchResult(t *testing.T) {
	st := store.NewMemStore()
	w, _ := st.CreateUser("w", "h")
	l, _ := st.CreateUser("l", "h")
	st.GetProfile(w)
	st.GetProfile(l)

	q := NewEventQueue(st, 16)
	q.Start()
	q.PushMatchResult(MatchResultEvent{
		WinnerUID: w, LoserUID: l, K: 32,
		RecordW: store.MatchRecord{UID: w, Result: 1},
		RecordL: store.MatchRecord{UID: l, Result: -1},
	})
	q.Stop() // Stop 保证排空

	pw, _ := st.GetProfile(w)
	pl, _ := st.GetProfile(l)
	if pw.Elo != 1016 || pl.Elo != 984 {
		t.Fatalf("w=%d l=%d", pw.Elo, pl.Elo)
	}
	if pw.Wins != 1 || pl.Losses != 1 {
		t.Fatalf("pw=%+v pl=%+v", pw, pl)
	}
}

func TestEventQueueStopIdempotent(t *testing.T) {
	st := store.NewMemStore()
	q := NewEventQueue(st, 4)
	q.Start()
	q.Stop()
	q.Stop() // 二次 Stop 不得 panic
}

func TestEventQueuePushAfterStop(t *testing.T) {
	st := store.NewMemStore()
	q := NewEventQueue(st, 4)
	q.Start()
	q.Stop()
	q.PushMatchResult(MatchResultEvent{}) // Stop 后 Push 不得 panic（丢弃）
}

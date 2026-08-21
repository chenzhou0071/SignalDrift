// eventqueue.go — 异步结算队列：单 worker 读档案/算 ELO/写库/战绩入库，onElo 回调推送；Stop 幂等防 closed channel panic
package lobby

import (
	"log"
	"sync"

	"signaldrift/server/internal/store"
)

type MatchResultEvent struct {
	WinnerUID, LoserUID int64
	Draw                bool
	K                   float64
	RecordW, RecordL    store.MatchRecord
}

type EventQueue struct {
	st store.Store
	ch chan MatchResultEvent
	wg sync.WaitGroup
	mu sync.Mutex
	// closed 置位后拒绝新事件：防 Stop 后 Push 向已关闭 channel 发送 panic
	closed bool
	// ELO 变动推送回调（结算面板用）；未设置则跳过
	onElo func(uid int64, oldElo, newElo int)
}

func NewEventQueue(st store.Store, size int) *EventQueue {
	return &EventQueue{st: st, ch: make(chan MatchResultEvent, size)}
}

// SetOnEloComputed 注入 ELO 变动回调：主进程装配时查 Presence 推 MsgEloUpdate
func (q *EventQueue) SetOnEloComputed(fn func(uid int64, oldElo, newElo int)) { q.onElo = fn }

func (q *EventQueue) Start() {
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		for ev := range q.ch {
			q.process(ev)
		}
	}()
}

func (q *EventQueue) Stop() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	close(q.ch)
	q.mu.Unlock()
	q.wg.Wait()
}

func (q *EventQueue) PushMatchResult(ev MatchResultEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return // 已关停：丢弃，不 panic
	}
	select {
	case q.ch <- ev:
	default:
		log.Printf("ERROR event queue full, drop match result w=%d l=%d", ev.WinnerUID, ev.LoserUID)
	}
}

func (q *EventQueue) process(ev MatchResultEvent) {
	pw, err := q.st.GetProfile(ev.WinnerUID)
	if err != nil {
		log.Printf("ERROR eq get winner profile: %v", err)
		return
	}
	pl, err := q.st.GetProfile(ev.LoserUID)
	if err != nil {
		log.Printf("ERROR eq get loser profile: %v", err)
		return
	}
	var nw, nl int
	resW, resL := 1, -1 // 胜/负
	if ev.Draw {
		nw, nl = NewEloDraw(pw.Elo, pl.Elo, ev.K)
		resW, resL = 0, 0 // 平局：双方胜负均不加
	} else {
		nw, nl = NewElo(pw.Elo, pl.Elo, ev.K)
	}
	if err := q.st.UpdateElo(ev.WinnerUID, nw, resW); err != nil {
		log.Printf("ERROR eq update winner elo: %v", err)
	}
	if err := q.st.UpdateElo(ev.LoserUID, nl, resL); err != nil {
		log.Printf("ERROR eq update loser elo: %v", err)
	}
	ev.RecordW.EloChange = nw - pw.Elo
	ev.RecordL.EloChange = nl - pl.Elo
	if err := q.st.InsertMatchRecord(&ev.RecordW); err != nil {
		log.Printf("ERROR eq insert record w: %v", err)
	}
	if err := q.st.InsertMatchRecord(&ev.RecordL); err != nil {
		log.Printf("ERROR eq insert record l: %v", err)
	}
	// ELO 变动推送（规格 2.7 结算面板 ELO 变化）；回调未设置则跳过
	if q.onElo != nil {
		q.onElo(ev.WinnerUID, pw.Elo, nw)
		q.onElo(ev.LoserUID, pl.Elo, nl)
	}
}

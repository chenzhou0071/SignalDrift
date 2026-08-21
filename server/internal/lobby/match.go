// match.go — 匹配池：ELO 排序相邻贪心配对，等待每满 WidenSec 放宽分差；取消/去重；时钟回拨不收紧
package lobby

import (
	"sort"
	"sync"
	"time"

	"signaldrift/server/internal/config"
)

type MatchPair struct {
	UIDA, UIDB int64
	EloA, EloB int
}

type matchEntry struct {
	uid     int64
	elo     int
	joinsAt int64
}

type MatchPool struct {
	mu      sync.Mutex
	cfg     config.MatchConfig
	entries map[int64]*matchEntry
	now     func() int64
}

func NewMatchPool(cfg config.MatchConfig, now func() int64) *MatchPool {
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}
	return &MatchPool{cfg: cfg, entries: make(map[int64]*matchEntry), now: now}
}

func (p *MatchPool) Add(uid int64, elo int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.entries[uid]; ok {
		return false
	}
	p.entries[uid] = &matchEntry{uid: uid, elo: elo, joinsAt: p.now()}
	return true
}

func (p *MatchPool) Cancel(uid int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.entries[uid]; !ok {
		return false
	}
	delete(p.entries, uid)
	return true
}

func (p *MatchPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

func (p *MatchPool) allowedGap(e *matchEntry, now int64) int {
	widen := int((now - e.joinsAt) / p.cfg.WidenSec)
	if widen < 0 { // 时钟回拨：等待时间为负，分差不得收紧
		widen = 0
	}
	return p.cfg.BaseGap + widen*p.cfg.GapStep
}

// Poll 按 ELO 排序贪心配对相邻者
func (p *MatchPool) Poll() []MatchPair {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	list := make([]*matchEntry, 0, len(p.entries))
	for _, e := range p.entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].elo < list[j].elo })

	var pairs []MatchPair
	used := make(map[int64]bool)
	for i := 0; i+1 < len(list); i++ {
		a, b := list[i], list[i+1]
		if used[a.uid] || used[b.uid] {
			continue
		}
		gap := b.elo - a.elo
		maxAllowed := p.allowedGap(a, now)
		if g := p.allowedGap(b, now); g > maxAllowed {
			maxAllowed = g
		}
		if gap <= maxAllowed {
			pairs = append(pairs, MatchPair{UIDA: a.uid, UIDB: b.uid, EloA: a.elo, EloB: b.elo})
			used[a.uid], used[b.uid] = true, true
		}
	}
	for uid := range used {
		delete(p.entries, uid)
	}
	return pairs
}

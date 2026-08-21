// presence.go — 在线索引：UID → Session，RWMutex 并发安全
package lobby

import (
	"sync"

	"signaldrift/server/internal/gateway"
)

type Presence struct {
	mu sync.RWMutex
	m  map[int64]*gateway.Session
}

func NewPresence() *Presence { return &Presence{m: make(map[int64]*gateway.Session)} }

func (p *Presence) Bind(uid int64, s *gateway.Session) {
	p.mu.Lock()
	p.m[uid] = s
	p.mu.Unlock()
}

func (p *Presence) Unbind(uid int64) {
	p.mu.Lock()
	delete(p.m, uid)
	p.mu.Unlock()
}

func (p *Presence) Get(uid int64) (*gateway.Session, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.m[uid]
	return s, ok
}

func (p *Presence) IsOnline(uid int64) bool {
	_, ok := p.Get(uid)
	return ok
}

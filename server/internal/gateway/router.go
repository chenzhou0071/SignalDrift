package gateway

import (
	"sync"
	"sync/atomic"

	"signaldrift/server/internal/protocol"
)

type HandlerFunc func(s *Session, f *protocol.Frame)

type Router struct {
	mu       sync.RWMutex
	handlers map[uint16]HandlerFunc
	unknown  atomic.Uint64
}

func NewRouter() *Router {
	return &Router{handlers: make(map[uint16]HandlerFunc)}
}

func (r *Router) Register(msgID uint16, h HandlerFunc) {
	r.mu.Lock()
	r.handlers[msgID] = h
	r.mu.Unlock()
}

func (r *Router) Dispatch(s *Session, f *protocol.Frame) {
	r.mu.RLock()
	h, ok := r.handlers[f.MsgID]
	r.mu.RUnlock()
	if !ok {
		r.unknown.Add(1)
		return
	}
	h(s, f)
}

func (r *Router) UnknownCount() uint64 { return r.unknown.Load() }

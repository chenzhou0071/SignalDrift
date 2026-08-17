package gateway

import (
	"net"
	"sync"
	"sync/atomic"

	"signaldrift/server/internal/protocol"
)

type Session struct {
	ID  uint64
	UID int64

	conn      net.Conn
	sendCh    chan []byte
	done      chan struct{}
	closeOnce sync.Once
	lastBeat  atomic.Int64
	lastSeq   atomic.Uint32
}

func (s *Session) Send(msgID uint16, body []byte) bool {
	if len(body) > protocol.MaxBodySize {
		return false // 超协议上限：不编码、不 panic
	}
	raw := protocol.Encode(&protocol.Frame{MsgID: msgID, Body: body})
	select {
	case s.sendCh <- raw:
		return true
	default:
		return false // 慢消费者：队满丢弃，由心跳超时最终清理
	}
}

func (s *Session) SendQueue() <-chan []byte { return s.sendCh }
func (s *Session) Done() <-chan struct{}    { return s.done }
func (s *Session) Touch(now int64)          { s.lastBeat.Store(now) }
func (s *Session) LastBeat() int64          { return s.lastBeat.Load() }

// CheckSeq 幂等过滤：客户端序列号必须严格递增。
func (s *Session) CheckSeq(seq uint32) bool {
	for {
		last := s.lastSeq.Load()
		if seq <= last {
			return false
		}
		if s.lastSeq.CompareAndSwap(last, seq) {
			return true
		}
	}
}

func (s *Session) RemoteIP() string {
	host, _, err := net.SplitHostPort(s.conn.RemoteAddr().String())
	if err != nil {
		return s.conn.RemoteAddr().String()
	}
	return host
}

func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.done)
		s.conn.Close()
	})
}

type SessionManager struct {
	mu        sync.RWMutex
	sessions  map[uint64]*Session
	nextID    uint64
	queueSize int
}

func NewSessionManager(queueSize int) *SessionManager {
	return &SessionManager{sessions: make(map[uint64]*Session), queueSize: queueSize}
}

func (m *SessionManager) Create(conn net.Conn) *Session {
	s := &Session{
		ID:     atomic.AddUint64(&m.nextID, 1),
		conn:   conn,
		sendCh: make(chan []byte, m.queueSize),
		done:   make(chan struct{}),
	}
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	return s
}

func (m *SessionManager) Get(id uint64) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *SessionManager) Remove(id uint64) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *SessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

func (m *SessionManager) Range(fn func(*Session) bool) {
	m.mu.RLock()
	snapshot := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		snapshot = append(snapshot, s)
	}
	m.mu.RUnlock()
	for _, s := range snapshot {
		if !fn(s) {
			return
		}
	}
}

package gateway

import (
	"bytes"
	"net"
	"testing"

	"signaldrift/server/internal/protocol"
)

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func newTestSession(t *testing.T) (*SessionManager, *Session) {
	t.Helper()
	m := NewSessionManager(8)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	return m, m.Create(c1)
}

func TestManagerCreateRemove(t *testing.T) {
	m, s := newTestSession(t)
	if m.Count() != 1 {
		t.Fatalf("count=%d", m.Count())
	}
	got, ok := m.Get(s.ID)
	if !ok || got != s {
		t.Fatal("Get failed")
	}
	m.Remove(s.ID)
	if m.Count() != 0 {
		t.Fatal("remove failed")
	}
}

func TestSendEnqueue(t *testing.T) {
	_, s := newTestSession(t)
	if !s.Send(protocol.MsgEcho, []byte("hi")) {
		t.Fatal("send should succeed")
	}
	raw := <-s.SendQueue()
	fr, err := protocol.NewFrameReader(bytesReader(raw)).Next()
	if err != nil || fr.MsgID != protocol.MsgEcho || string(fr.Body) != "hi" {
		t.Fatalf("fr=%+v err=%v", fr, err)
	}
}

func TestSendQueueFull(t *testing.T) {
	m := NewSessionManager(1)
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	s := m.Create(c1)
	s.Send(1, nil) // 占满
	if s.Send(1, nil) {
		t.Fatal("expected queue full -> false")
	}
}

func TestCheckSeqMonotonic(t *testing.T) {
	_, s := newTestSession(t)
	if !s.CheckSeq(1) || !s.CheckSeq(2) || !s.CheckSeq(10) {
		t.Fatal("increasing seq must pass")
	}
	if s.CheckSeq(10) || s.CheckSeq(5) {
		t.Fatal("duplicate/backward seq must fail")
	}
}

func TestCloseIdempotent(t *testing.T) {
	_, s := newTestSession(t)
	s.Close()
	s.Close() // 不 panic
	select {
	case <-s.Done():
	default:
		t.Fatal("Done should be closed")
	}
}

func TestSendRejectsOversizedBody(t *testing.T) {
	_, s := newTestSession(t)
	if s.Send(1, make([]byte, protocol.MaxBodySize+1)) {
		t.Fatal("oversized body must not be enqueued")
	}
	if !s.Send(1, make([]byte, protocol.MaxBodySize)) {
		t.Fatal("body at MaxBodySize must be accepted")
	}
}

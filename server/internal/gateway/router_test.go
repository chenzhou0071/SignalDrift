package gateway

import (
	"net"
	"testing"

	"signaldrift/server/internal/protocol"
)

func TestRouterDispatch(t *testing.T) {
	m := NewSessionManager(8)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	s := m.Create(c1)

	r := NewRouter()
	var got *protocol.Frame
	r.Register(protocol.MsgEcho, func(sess *Session, f *protocol.Frame) { got = f })

	r.Dispatch(s, &protocol.Frame{MsgID: protocol.MsgEcho, Seq: 1, Body: []byte("x")})
	if got == nil || string(got.Body) != "x" {
		t.Fatalf("handler not called: %+v", got)
	}
}

func TestRouterUnknown(t *testing.T) {
	m := NewSessionManager(8)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	s := m.Create(c1)

	r := NewRouter()
	r.Dispatch(s, &protocol.Frame{MsgID: 999}) // 不 panic
	if r.UnknownCount() != 1 {
		t.Fatalf("unknown=%d", r.UnknownCount())
	}
}

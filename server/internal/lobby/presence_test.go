package lobby

import (
	"net"
	"testing"

	"signaldrift/server/internal/gateway"
)

func TestPresenceBindUnbind(t *testing.T) {
	m := gateway.NewSessionManager(4)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	s := m.Create(c1)

	p := NewPresence()
	p.Bind(7, s)
	if !p.IsOnline(7) {
		t.Fatal("must be online")
	}
	got, ok := p.Get(7)
	if !ok || got != s {
		t.Fatal("Get failed")
	}
	p.Unbind(7)
	if p.IsOnline(7) {
		t.Fatal("must be offline")
	}
}

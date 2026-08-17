package gateway

import (
	"net"
	"testing"
)

func TestSweepTimeout(t *testing.T) {
	m := NewSessionManager(8)
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	s := m.Create(c1)

	var fakeNow int64 = 100
	s.Touch(fakeNow)
	var timedOut []*Session
	w := NewHeartbeatWatcher(m, 15, func(sess *Session) {
		timedOut = append(timedOut, sess)
	}, func() int64 { return fakeNow })

	w.Sweep()
	if len(timedOut) != 0 {
		t.Fatal("fresh session must not time out")
	}

	fakeNow = 116 // 超过 15 秒
	w.Sweep()
	if len(timedOut) != 1 || timedOut[0] != s {
		t.Fatalf("expected timeout of s, got %v", timedOut)
	}
}

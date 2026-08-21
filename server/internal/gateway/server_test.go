// server_test.go — 服务器集成测试：Echo 全链路、限流踢人、Stop 关闭所有连接
package gateway

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/protocol"
)

func startTestServer(t *testing.T, rate, burst float64) *Server {
	t.Helper()
	cfg := &config.ServerConfig{
		ListenAddr:    "127.0.0.1:0",
		MaxConns:      16,
		SendQueueSize: 16,
		IPRate:        config.RateConfig{Rate: rate, Burst: burst},
		Heartbeat:     config.HeartbeatConfig{IntervalSec: 5, TimeoutSec: 15, SweepSec: 1},
	}
	srv := NewServer(cfg, NewRouter())
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	return srv
}

func dial(t *testing.T, srv *Server) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestHeartbeatAck(t *testing.T) {
	srv := startTestServer(t, 100, 100)
	c := dial(t, srv)
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 1}))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	f, err := protocol.NewFrameReader(c).Next()
	if err != nil || f.MsgID != protocol.MsgHeartbeatAck {
		t.Fatalf("f=%+v err=%v", f, err)
	}
}

func TestDuplicateSeqDropped(t *testing.T) {
	srv := startTestServer(t, 100, 100)
	c := dial(t, srv)
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 5}))
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 5})) // 重复
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 6}))
	fr := protocol.NewFrameReader(c)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	fr.Next() // ack for seq5
	fr.Next() // ack for seq6
	// 第三个 ack 不应存在：短暂等待后读取必须超时
	c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := fr.Next(); err == nil {
		t.Fatal("duplicate seq must not produce a 3rd ack")
	}
}

func TestRateLimitKick(t *testing.T) {
	srv := startTestServer(t, 1, 2) // 极小限额
	c := dial(t, srv)
	for i := 1; i <= 3; i++ {
		c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: uint32(i)}))
	}
	// 超限后服务端应断开：持续读最终得到错误
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	fr := protocol.NewFrameReader(c)
	for {
		if _, err := fr.Next(); err != nil {
			if errors.Is(err, io.EOF) {
				return // 服务端主动关闭连接，符合预期
			}
			t.Fatalf("expected kick (EOF), got %v", err)
		}
	}
}
func TestStopClosesAllConns(t *testing.T) {
	srv := startTestServer(t, 100, 100)
	c := dial(t, srv)
	// 确认连接已建立并被服务端处理
	c.Write(protocol.Encode(&protocol.Frame{MsgID: protocol.MsgHeartbeat, Seq: 1}))
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := protocol.NewFrameReader(c).Next(); err != nil {
		t.Fatalf("heartbeat ack: %v", err)
	}
	// Stop 必须正常返回（不挂起）
	done := make(chan struct{})
	go func() { srv.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() hung")
	}
	// 已建立连接被服务端关闭：读到 EOF
	buf := make([]byte, 1)
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after Stop, got %v", err)
	}
}

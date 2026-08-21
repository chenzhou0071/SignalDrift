// server.go — TCP 接入层：accept、每连接读写循环、限流断连、心跳应答、优雅关闭（Stop 关停竞态防护）
package gateway

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/protocol"
)

type Server struct {
	cfg        *config.ServerConfig
	router     *Router
	mgr        *SessionManager
	limiter    *IPLimiter
	listener   net.Listener
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	acceptDone chan struct{}
}

func NewServer(cfg *config.ServerConfig, router *Router) *Server {
	srv := &Server{
		cfg:     cfg,
		router:  router,
		mgr:     NewSessionManager(cfg.SendQueueSize),
		limiter: NewIPLimiter(cfg.IPRate.Rate, cfg.IPRate.Burst, nil),
	}
	// 网关内建：心跳应答
	router.Register(protocol.MsgHeartbeat, func(s *Session, f *protocol.Frame) {
		s.Touch(time.Now().Unix())
		s.Send(protocol.MsgHeartbeatAck, nil)
	})
	return srv
}

func (srv *Server) Sessions() *SessionManager { return srv.mgr }
func (srv *Server) Addr() net.Addr            { return srv.listener.Addr() }

func (srv *Server) Start() error {
	ln, err := net.Listen("tcp", srv.cfg.ListenAddr)
	if err != nil {
		return err
	}
	srv.listener = ln
	ctx, cancel := context.WithCancel(context.Background())
	srv.cancel = cancel

	watcher := NewHeartbeatWatcher(srv.mgr, int64(srv.cfg.Heartbeat.TimeoutSec), func(s *Session) {
		log.Printf("WARN session %d heartbeat timeout", s.ID)
		s.Close()
	}, nil)
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		watcher.Run(ctx, time.Duration(srv.cfg.Heartbeat.SweepSec)*time.Second)
	}()

	acceptDone := make(chan struct{})
	srv.acceptDone = acceptDone
	srv.wg.Add(1)
	go func() {
		defer close(acceptDone) // acceptLoop 退出后，Stop 才可确定不会再有新 session
		srv.acceptLoop()
	}()
	return nil
}

func (srv *Server) acceptLoop() {
	defer srv.wg.Done()
	for {
		conn, err := srv.listener.Accept()
		if err != nil {
			// 瞬时错误（如 accept 超时）：重试而非退出
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// 其余错误：监听器已关闭（Stop 路径）或致命错误，退出
			log.Printf("WARN accept error: %v", err)
			return
		}
		if srv.mgr.Count() >= srv.cfg.MaxConns {
			log.Printf("WARN max conns reached, reject %s", conn.RemoteAddr())
			conn.Close()
			continue
		}
		s := srv.mgr.Create(conn)
		s.Touch(time.Now().Unix())
		srv.wg.Add(2)
		go srv.writeLoop(s, conn)
		go srv.readLoop(s, conn)
	}
}

func (srv *Server) readLoop(s *Session, conn net.Conn) {
	defer srv.wg.Done()
	defer srv.teardown(s)
	fr := protocol.NewFrameReader(conn)
	ip := s.RemoteIP()
	for {
		f, err := fr.Next()
		if err != nil {
			return // 断开或协议错误
		}
		if !srv.limiter.Allow(ip) {
			log.Printf("WARN rate limit kick session=%d ip=%s", s.ID, ip)
			return
		}
		if !s.CheckSeq(f.Seq) {
			continue // 重复/回退包：静默丢弃
		}
		srv.router.Dispatch(s, f)
	}
}

func (srv *Server) writeLoop(s *Session, conn net.Conn) {
	defer srv.wg.Done()
	for {
		select {
		case <-s.Done():
			return
		case raw := <-s.SendQueue():
			if _, err := conn.Write(raw); err != nil {
				s.Close()
				return
			}
		}
	}
}

func (srv *Server) teardown(s *Session) {
	s.Close()
	srv.mgr.Remove(s.ID)
}

func (srv *Server) Stop() {
	if srv.cancel != nil {
		srv.cancel()
	}
	if srv.listener != nil {
		srv.listener.Close()
	}
	// 等 acceptLoop 退出：此后不会再有新 session，Range 快照不漏关
	if srv.acceptDone != nil {
		<-srv.acceptDone
	}
	srv.mgr.Range(func(s *Session) bool { s.Close(); return true })
	srv.wg.Wait()
}

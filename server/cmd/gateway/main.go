// main.go — 进程入口：加载配置、起网关、监听信号、优雅关闭
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/gateway"
	"signaldrift/server/internal/protocol"
)

func main() {
	cfgPath := flag.String("config", "configs/server.json", "config file path")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("ERROR load config: %v", err)
	}

	router := gateway.NewRouter()
	// 临时 Echo：验证全链路，大厅服务接入后移除
	router.Register(protocol.MsgEcho, func(s *gateway.Session, f *protocol.Frame) {
		s.Send(protocol.MsgEcho, f.Body)
	})

	srv := gateway.NewServer(cfg, router)
	if err := srv.Start(); err != nil {
		log.Fatalf("ERROR start: %v", err)
	}
	log.Printf("INFO gateway listening on %s", srv.Addr())

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("INFO shutting down...")
	srv.Stop()
	log.Println("INFO gateway stopped")
}

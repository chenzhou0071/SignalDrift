// main.go — 进程入口：加载配置、装配大厅（MySQL/事件队列/匹配循环）、起网关、监听信号、优雅关闭
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"signaldrift/server/internal/config"
	"signaldrift/server/internal/gateway"
	"signaldrift/server/internal/lobby"
	"signaldrift/server/internal/store"
)

func main() {
	cfgPath := flag.String("config", "configs/server.json", "config file path")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("ERROR load config: %v", err)
	}

	// 大厅装配：配置 → MySQL → 事件队列 → Service → 路由挂载
	lobbyCfg, err := config.LoadLobby("configs/lobby.json")
	if err != nil {
		log.Fatalf("ERROR load lobby config: %v", err)
	}
	st, err := store.NewMySQL(lobbyCfg.MysqlDSN)
	if err != nil {
		log.Fatalf("ERROR mysql: %v", err)
	}
	eq := lobby.NewEventQueue(st, lobbyCfg.QueueSize)
	eq.Start()
	svc := lobby.NewService(lobbyCfg, st,
		lobby.NewMatchPool(lobbyCfg.Match, nil), lobby.NewPresence(), eq,
		lobby.NewTokenIssuer(lobbyCfg.TokenSecret, lobbyCfg.TokenTTLSec, nil))

	router := gateway.NewRouter()
	svc.Mount(router)

	srv := gateway.NewServer(cfg, router)
	srv.SetOnSessionClosed(svc.OnSessionClosed)
	if err := srv.Start(); err != nil {
		log.Fatalf("ERROR start: %v", err)
	}
	log.Printf("INFO gateway listening on %s", srv.Addr())

	ctx, cancelMatch := context.WithCancel(context.Background())
	go svc.RunMatchLoop(ctx, time.Second)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("INFO shutting down...")
	// 退出顺序：先停匹配循环，再关网关（断连回调清大厅状态），最后排空事件队列
	cancelMatch()
	srv.Stop()
	eq.Stop()
	log.Println("INFO gateway stopped")
}

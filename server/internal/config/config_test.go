package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "server.json")
	data := `{"listen_addr":"127.0.0.1:9000","max_conns":100,"send_queue_size":64,
"ip_rate":{"rate":50,"burst":80},"heartbeat":{"interval_sec":5,"timeout_sec":15,"sweep_sec":1}}`
	if err := os.WriteFile(p, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != "127.0.0.1:9000" || cfg.MaxConns != 100 ||
		cfg.IPRate.Burst != 80 || cfg.Heartbeat.TimeoutSec != 15 {
		t.Fatalf("bad cfg: %+v", cfg)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("no/such/file.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

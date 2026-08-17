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

func TestLoadRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	bad := []struct{ name, json string }{
		{"rate_zero", "{\"listen_addr\":\"127.0.0.1:9000\",\"max_conns\":100,\"send_queue_size\":64,\"ip_rate\":{\"rate\":0,\"burst\":80},\"heartbeat\":{\"interval_sec\":5,\"timeout_sec\":15,\"sweep_sec\":1}}"},
		{"burst_zero", "{\"listen_addr\":\"127.0.0.1:9000\",\"max_conns\":100,\"send_queue_size\":64,\"ip_rate\":{\"rate\":50,\"burst\":0},\"heartbeat\":{\"interval_sec\":5,\"timeout_sec\":15,\"sweep_sec\":1}}"},

		{"max_conns_zero", "{\"listen_addr\":\"127.0.0.1:9000\",\"max_conns\":0,\"send_queue_size\":64,\"ip_rate\":{\"rate\":50,\"burst\":80},\"heartbeat\":{\"interval_sec\":5,\"timeout_sec\":15,\"sweep_sec\":1}}"},
		{"send_queue_zero", "{\"listen_addr\":\"127.0.0.1:9000\",\"max_conns\":100,\"send_queue_size\":0,\"ip_rate\":{\"rate\":50,\"burst\":80},\"heartbeat\":{\"interval_sec\":5,\"timeout_sec\":15,\"sweep_sec\":1}}"},

		{"timeout_zero", "{\"listen_addr\":\"127.0.0.1:9000\",\"max_conns\":100,\"send_queue_size\":64,\"ip_rate\":{\"rate\":50,\"burst\":80},\"heartbeat\":{\"interval_sec\":5,\"timeout_sec\":0,\"sweep_sec\":1}}"},
		{"empty_listen", "{\"listen_addr\":\"\",\"max_conns\":100,\"send_queue_size\":64,\"ip_rate\":{\"rate\":50,\"burst\":80},\"heartbeat\":{\"interval_sec\":5,\"timeout_sec\":15,\"sweep_sec\":1}}"},
	}

	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name+".json")
			if err := os.WriteFile(p, []byte(tc.json), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
	valid := "{\"listen_addr\":\"127.0.0.1:9000\",\"max_conns\":100,\"send_queue_size\":64,\"ip_rate\":{\"rate\":50,\"burst\":80},\"heartbeat\":{\"interval_sec\":5,\"timeout_sec\":15,\"sweep_sec\":1}}"
	p := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(p, []byte(valid), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

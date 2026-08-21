// lobby_test.go — 大厅配置加载测试：全字段断言/缺文件/非法值（表驱动）
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLobby(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "lobby.json")
	data := `{"mysql_dsn":"dsn","token_secret":"s","token_ttl_sec":60,
"match":{"base_gap":100,"gap_step":100,"widen_sec":30},"elo_k":32,"queue_size":16}`
	if err := os.WriteFile(p, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadLobby(p)
	if err != nil {
		t.Fatalf("LoadLobby: %v", err)
	}
	if c.MysqlDSN != "dsn" || c.TokenSecret != "s" || c.TokenTTLSec != 60 ||
		c.Match.BaseGap != 100 || c.Match.GapStep != 100 || c.Match.WidenSec != 30 ||
		c.EloK != 32 || c.QueueSize != 16 {
		t.Fatalf("bad cfg: %+v", c)
	}
}

func TestLoadLobbyMissingFile(t *testing.T) {
	if _, err := LoadLobby("no/such/lobby.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadLobbyRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	valid := `{"mysql_dsn":"dsn","token_secret":"s","token_ttl_sec":60,
"match":{"base_gap":100,"gap_step":100,"widen_sec":30},"elo_k":32,"queue_size":16}`
	bad := []struct{ name, json string }{
		{"empty_dsn", `{"mysql_dsn":"","token_ttl_sec":60,"queue_size":16,"elo_k":32}`},
		{"ttl_zero", `{"mysql_dsn":"dsn","token_ttl_sec":0,"queue_size":16,"elo_k":32}`},
		{"elo_zero", `{"mysql_dsn":"dsn","token_ttl_sec":60,"queue_size":16,"elo_k":0}`},
		{"queue_zero", `{"mysql_dsn":"dsn","token_ttl_sec":60,"queue_size":0,"elo_k":32}`},
		{"widen_zero", `{"mysql_dsn":"dsn","token_ttl_sec":60,"queue_size":16,"elo_k":32,"match":{"base_gap":100,"gap_step":100,"widen_sec":0}}`},
	}

	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, tc.name+".json")
			if err := os.WriteFile(p, []byte(tc.json), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadLobby(p); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
	p := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(p, []byte(valid), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLobby(p); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

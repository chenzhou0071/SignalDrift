// lobby.go — 大厅配置：LobbyConfig/MatchConfig 结构、加载与校验（MySQL DSN/token/匹配/ELO）
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type MatchConfig struct {
	BaseGap  int   `json:"base_gap"`
	GapStep  int   `json:"gap_step"`
	WidenSec int64 `json:"widen_sec"`
}

type LobbyConfig struct {
	MysqlDSN    string      `json:"mysql_dsn"`
	TokenSecret string      `json:"token_secret"`
	TokenTTLSec int64       `json:"token_ttl_sec"`
	Match       MatchConfig `json:"match"`
	EloK        float64     `json:"elo_k"`
	QueueSize   int         `json:"queue_size"`
}

func LoadLobby(path string) (*LobbyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c LobbyConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *LobbyConfig) validate() error {
	if c.MysqlDSN == "" {
		return fmt.Errorf("config: mysql_dsn must not be empty")
	}
	if c.TokenTTLSec <= 0 {
		return fmt.Errorf("config: token_ttl_sec must be > 0, got %d", c.TokenTTLSec)
	}
	if c.EloK <= 0 {
		return fmt.Errorf("config: elo_k must be > 0, got %v", c.EloK)
	}
	if c.QueueSize <= 0 {
		return fmt.Errorf("config: queue_size must be > 0, got %d", c.QueueSize)
	}
	if c.Match.BaseGap <= 0 {
		return fmt.Errorf("config: match.base_gap must be > 0, got %d", c.Match.BaseGap)
	}
	if c.Match.GapStep <= 0 {
		return fmt.Errorf("config: match.gap_step must be > 0, got %d", c.Match.GapStep)
	}
	if c.Match.WidenSec <= 0 {
		return fmt.Errorf("config: match.widen_sec must be > 0, got %d", c.Match.WidenSec)
	}
	return nil
}

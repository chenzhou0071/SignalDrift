package config

import (
	"encoding/json"
	"os"
)

type RateConfig struct {
	Rate  float64 `json:"rate"`
	Burst float64 `json:"burst"`
}

type HeartbeatConfig struct {
	IntervalSec int `json:"interval_sec"`
	TimeoutSec  int `json:"timeout_sec"`
	SweepSec    int `json:"sweep_sec"`
}

type ServerConfig struct {
	ListenAddr    string          `json:"listen_addr"`
	MaxConns      int             `json:"max_conns"`
	SendQueueSize int             `json:"send_queue_size"`
	IPRate        RateConfig      `json:"ip_rate"`
	Heartbeat     HeartbeatConfig `json:"heartbeat"`
}

func Load(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

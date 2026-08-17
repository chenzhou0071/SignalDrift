package config

import (
	"encoding/json"
	"fmt"
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
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (cfg *ServerConfig) validate() error {
	if cfg.ListenAddr == "" {
		return fmt.Errorf("config: listen_addr must not be empty")
	}
	if cfg.MaxConns <= 0 {
		return fmt.Errorf("config: max_conns must be > 0, got %d", cfg.MaxConns)
	}
	if cfg.SendQueueSize <= 0 {
		return fmt.Errorf("config: send_queue_size must be > 0, got %d", cfg.SendQueueSize)
	}
	if cfg.IPRate.Rate <= 0 {
		return fmt.Errorf("config: ip_rate.rate must be > 0, got %v", cfg.IPRate.Rate)
	}
	if cfg.IPRate.Burst <= 0 {
		return fmt.Errorf("config: ip_rate.burst must be > 0, got %v", cfg.IPRate.Burst)
	}
	if cfg.Heartbeat.IntervalSec <= 0 {
		return fmt.Errorf("config: heartbeat.interval_sec must be > 0, got %d", cfg.Heartbeat.IntervalSec)
	}
	if cfg.Heartbeat.TimeoutSec <= 0 {
		return fmt.Errorf("config: heartbeat.timeout_sec must be > 0, got %d", cfg.Heartbeat.TimeoutSec)
	}
	if cfg.Heartbeat.SweepSec <= 0 {
		return fmt.Errorf("config: heartbeat.sweep_sec must be > 0, got %d", cfg.Heartbeat.SweepSec)
	}
	return nil
}
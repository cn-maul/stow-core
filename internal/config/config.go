package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Addr string   `json:"addr"`
	DB   string   `json:"db"`
	Keys []string `json:"keys"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:8080"
	}
	if cfg.DB == "" {
		cfg.DB = "data/stow.db"
	}

	return &cfg, nil
}

func IsValidKey(key string) bool {
	return len(key) == 11 && key[:5] == "stow-"
}

func ValidateKeys(keys []string) error {
	for _, key := range keys {
		if !IsValidKey(key) {
			return fmt.Errorf("invalid key format: %s (expected stow-xxxxxx, 6 alphanumeric chars)", key)
		}
	}
	return nil
}

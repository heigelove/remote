package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	DB     DBConfig     `yaml:"db"`
	JWT    JWTConfig    `yaml:"jwt"`
	Seed   SeedConfig   `yaml:"seed"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
	Mode string `yaml:"mode"`
}

type DBConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type JWTConfig struct {
	Secret           string `yaml:"secret"`
	AccessTTLMinutes int    `yaml:"access_ttl_minutes"`
	RefreshTTLHours  int    `yaml:"refresh_ttl_hours"`
}

type SeedConfig struct {
	AdminUsername string `yaml:"admin_username"`
	AdminPassword string `yaml:"admin_password"`
	DemoUsername  string `yaml:"demo_username"`
	DemoPassword  string `yaml:"demo_password"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Server.Addr == "" {
		cfg.Server.Addr = ":8080"
	}
	if cfg.DB.Driver == "" {
		cfg.DB.Driver = "sqlite"
	}
	if cfg.DB.DSN == "" {
		cfg.DB.DSN = "remoto.db"
	}
	if cfg.JWT.Secret == "" {
		cfg.JWT.Secret = "dev-secret"
	}
	if cfg.JWT.AccessTTLMinutes == 0 {
		cfg.JWT.AccessTTLMinutes = 120
	}
	if cfg.JWT.RefreshTTLHours == 0 {
		cfg.JWT.RefreshTTLHours = 168
	}
	return &cfg, nil
}

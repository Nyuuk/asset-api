package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server struct {
		ListenAddr string `yaml:"listen_addr"`
		BaseURL    string `yaml:"base_url"`
	} `yaml:"server"`
	S3 struct {
		Endpoint       string `yaml:"endpoint"`
		Region         string `yaml:"region"`
		Bucket         string `yaml:"bucket"`
		ForcePathStyle bool   `yaml:"force_path_style"`
		AccessKey      string `yaml:"access_key"`
		SecretKey      string `yaml:"secret_key"`
	} `yaml:"s3"`
	Database struct {
		DSN string `yaml:"dsn"`
	} `yaml:"database"`
	TTL struct {
		DefaultSeconds         int64 `yaml:"default_seconds"`
		MaxSeconds             int64 `yaml:"max_seconds"`
		CleanupIntervalSeconds int64 `yaml:"cleanup_interval_seconds"`
	} `yaml:"ttl"`
	Limits struct {
		MaxFileSizeMB int64 `yaml:"max_file_size_mb"`
	} `yaml:"limits"`
	Admin struct {
		Token string `yaml:"token"`
	} `yaml:"admin"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.TTL.CleanupIntervalSeconds <= 0 {
		cfg.TTL.CleanupIntervalSeconds = 300
	}
	return &cfg, nil
}

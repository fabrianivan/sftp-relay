package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type SFTPConfig struct {
	Host                string `json:"host"`
	Port                int    `json:"port"`
	Username            string `json:"username"`
	Password            string `json:"password"`
	TargetDirectory     string `json:"targetDirectory"`
	ExpectedFingerprint string `json:"expectedFingerprint"`
}

type ScheduleConfig struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
}

type Config struct {
	SourceSFTP         SFTPConfig     `json:"sourceSFTP"`
	TargetSFTP         SFTPConfig     `json:"targetSFTP"`
	BackupDirectory    string         `json:"backupDirectory"`
	IdleTimeoutSeconds int            `json:"idleTimeoutSeconds"`
	ReconnectInterval  int            `json:"reconnectInterval"`
	ReconnectRetries   int            `json:"reconnectRetries"`
	LogFile            string         `json:"logFile"`
	LogRetentionDays   int            `json:"logRetentionDays"`
	ShowBalloonTimeout int            `json:"showBalloonTimeout"`
	PollInterval       int            `json:"pollInterval"`
	ActiveSchedule     ScheduleConfig `json:"activeSchedule"`
	KeepAliveDuration  int            `json:"keepAliveDuration"`
	EnableInitialSync  *bool          `json:"enableInitialSync,omitempty"`
	MaxIdleScans       int            `json:"maxIdleScans"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	applyDefaults(&cfg)
	if err := validateSchedule(cfg.ActiveSchedule); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.IdleTimeoutSeconds == 0 {
		cfg.IdleTimeoutSeconds = 30
	}
	if cfg.ReconnectInterval == 0 {
		cfg.ReconnectInterval = 10
	}
	if cfg.ReconnectRetries == 0 {
		cfg.ReconnectRetries = 2
	}
	if cfg.LogFile == "" {
		cfg.LogFile = "sftp_uploader.log"
	}
	if cfg.LogRetentionDays == 0 {
		cfg.LogRetentionDays = 1
	}
	if cfg.ShowBalloonTimeout == 0 {
		cfg.ShowBalloonTimeout = 10
	}
	if cfg.ActiveSchedule == (ScheduleConfig{}) {
		cfg.ActiveSchedule = ScheduleConfig{
			Enabled:  false,
			Start:    "00:00",
			End:      "23:59",
			Timezone: "Local",
		}
	}
	if cfg.KeepAliveDuration == 0 {
		cfg.KeepAliveDuration = 30
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 30
	}
	if cfg.MaxIdleScans == 0 {
		cfg.MaxIdleScans = 10
	}
	if cfg.EnableInitialSync == nil {
		defaultVal := true
		cfg.EnableInitialSync = &defaultVal
	}
}

func validateSchedule(schedule ScheduleConfig) error {
	if !schedule.Enabled {
		return nil
	}

	startTime, err := time.ParseInLocation("15:04", schedule.Start, time.Local)
	if err != nil {
		return fmt.Errorf("invalid start time: %w", err)
	}
	endTime, err := time.ParseInLocation("15:04", schedule.End, time.Local)
	if err != nil {
		return fmt.Errorf("invalid end time: %w", err)
	}
	if endTime.Before(startTime) {
		return fmt.Errorf("end time must be after start time")
	}
	return nil
}

func DefaultConfig() *Config {
	return &Config{
		SourceSFTP: SFTPConfig{
			Host:            "localhost",
			Port:            22,
			Username:        "username",
			Password:        "password",
			TargetDirectory: "/files/ftp_folder/upload",
		},
		TargetSFTP: SFTPConfig{
			Host:            "localhost",
			Port:            22,
			Username:        "username",
			Password:        "password",
			TargetDirectory: "/upload",
		},
		BackupDirectory:    "/files/ftp_folder/backup",
		IdleTimeoutSeconds: 300,
		ReconnectInterval:  10,
		ReconnectRetries:   5,
		LogFile:            "sftp_uploader.log",
		LogRetentionDays:   7,
		ShowBalloonTimeout: 10,
		PollInterval:       30,
		MaxIdleScans:       10,
	}
}

func WriteDefaultConfig(path string) error {
	cfg := DefaultConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

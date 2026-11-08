// Package config loads, validates and saves the SFTP Relay configuration.
//
// The JSON format is backward compatible with config files written by
// SFTP Watchdog (the project's previous name): legacy keys such as
// "targetDirectory" are migrated on load.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Endpoint describes one SFTP server.
type Endpoint struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`

	// Password is only kept here when the user opts out of the OS keychain.
	// Prefer the keychain or the SFTP_RELAY_<ROLE>_PASSWORD environment variable.
	Password string `json:"password,omitempty"`

	// PrivateKeyPath enables public-key authentication (OpenSSH/PEM format).
	PrivateKeyPath string `json:"privateKeyPath,omitempty"`

	// Directory is the folder to watch (source) or deliver into (target).
	Directory string `json:"directory"`

	// ExpectedFingerprint pins the server host key (e.g. "SHA256:abc...").
	// When empty, the host key is verified against known_hosts (trust on first use).
	ExpectedFingerprint string `json:"expectedFingerprint,omitempty"`

	// LegacyTargetDirectory is the pre-v2 name of Directory. Read-only.
	LegacyTargetDirectory string `json:"targetDirectory,omitempty"`
}

// Enabled reports whether the endpoint is configured.
func (e Endpoint) Enabled() bool { return strings.TrimSpace(e.Host) != "" }

// Addr returns host:port.
func (e Endpoint) Addr() string { return net.JoinHostPort(e.Host, strconv.Itoa(e.Port)) }

// Schedule restricts automatic scans to a daily time window.
type Schedule struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start"`    // HH:MM
	End      string `json:"end"`      // HH:MM, may be earlier than Start for overnight windows
	Timezone string `json:"timezone"` // IANA name or "Local"
}

// Config is the top-level application configuration.
type Config struct {
	Source          Endpoint `json:"sourceSFTP"`
	Target          Endpoint `json:"targetSFTP"`
	BackupDirectory string   `json:"backupDirectory"`

	// Transfer rules.
	PollInterval   int      `json:"pollInterval"`             // seconds between scans
	StableSeconds  int      `json:"stableSeconds"`            // a file must be unchanged this long before it is picked up (0 = default 10s, -1 = off)
	MaxParallel    int      `json:"maxParallel"`              // concurrent transfers
	VerifyChecksum *bool    `json:"verifyChecksum,omitempty"` // re-read the uploaded file and compare SHA-256
	Include        []string `json:"include,omitempty"`        // glob patterns, default ["*"]
	Exclude        []string `json:"exclude,omitempty"`        // glob patterns, default temp/hidden files

	// Connection handling.
	ReconnectInterval     int    `json:"reconnectInterval"` // seconds, base for exponential backoff
	ReconnectRetries      int    `json:"reconnectRetries"`
	KeepAliveDuration     int    `json:"keepAliveDuration"` // seconds
	MaxIdleScans          int    `json:"maxIdleScans"`      // close idle sessions after N empty scans (0 = never)
	StrictHostKeyChecking bool   `json:"strictHostKeyChecking"`
	KnownHostsFile        string `json:"knownHostsFile,omitempty"`

	// Housekeeping.
	LogFile          string `json:"logFile"`
	LogRetentionDays int    `json:"logRetentionDays"`
	HistoryFile      string `json:"historyFile,omitempty"`
	HistoryLimit     int    `json:"historyLimit,omitempty"`

	ActiveSchedule    Schedule `json:"activeSchedule"`
	EnableInitialSync *bool    `json:"enableInitialSync,omitempty"`
	Notifications     *bool    `json:"notifications,omitempty"`
}

// DefaultExclude skips hidden files and common in-progress upload names.
var DefaultExclude = []string{".*", "~*", "*.tmp", "*.part", "*.partial", "*.filepart", "*.crdownload"}

// Load reads, migrates, defaults and validates a config file.
func Load(p string) (*Config, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	cfg.migrate()
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return &cfg, err
	}
	return &cfg, nil
}

// Save writes the config atomically with owner-only permissions.
func Save(p string, cfg *Config) error {
	c := *cfg
	c.Source.LegacyTargetDirectory = ""
	c.Target.LegacyTargetDirectory = ""
	data, err := json.MarshalIndent(&c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if dir := filepath.Dir(p); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Example returns a starter configuration with placeholders.
func Example() *Config {
	t := true
	cfg := &Config{
		Source: Endpoint{
			Host:      "source.example.com",
			Port:      22,
			Username:  "relay",
			Directory: "/outbox",
		},
		Target: Endpoint{
			Host:      "target.example.com",
			Port:      22,
			Username:  "relay",
			Directory: "/inbox",
		},
		BackupDirectory:   "/outbox/archive",
		VerifyChecksum:    &t,
		EnableInitialSync: &t,
		Notifications:     &t,
		ActiveSchedule:    Schedule{Enabled: false, Start: "06:00", End: "22:00", Timezone: "Local"},
	}
	cfg.ApplyDefaults()
	return cfg
}

func (c *Config) migrate() {
	for _, e := range []*Endpoint{&c.Source, &c.Target} {
		if e.Directory == "" && e.LegacyTargetDirectory != "" {
			e.Directory = e.LegacyTargetDirectory
		}
		e.LegacyTargetDirectory = ""
	}
}

// ApplyDefaults fills zero values with sensible defaults.
func (c *Config) ApplyDefaults() {
	for _, e := range []*Endpoint{&c.Source, &c.Target} {
		e.Host = strings.TrimSpace(e.Host)
		if e.Port == 0 {
			e.Port = 22
		}
	}
	setDefault(&c.PollInterval, 30)
	if c.StableSeconds == 0 {
		c.StableSeconds = 10
	}
	setDefault(&c.MaxParallel, 2)
	setDefault(&c.ReconnectInterval, 5)
	setDefault(&c.ReconnectRetries, 3)
	setDefault(&c.KeepAliveDuration, 30)
	setDefault(&c.LogRetentionDays, 14)
	setDefault(&c.HistoryLimit, 1000)
	if c.MaxIdleScans < 0 {
		c.MaxIdleScans = 0
	}
	if c.LogFile == "" {
		c.LogFile = "logs/sftp-relay.log"
	}
	if c.HistoryFile == "" {
		c.HistoryFile = "history.json"
	}
	if c.KnownHostsFile == "" {
		c.KnownHostsFile = "known_hosts"
	}
	if len(c.Include) == 0 {
		c.Include = []string{"*"}
	}
	if c.Exclude == nil {
		c.Exclude = append([]string(nil), DefaultExclude...)
	}
	if c.ActiveSchedule.Start == "" {
		c.ActiveSchedule.Start = "00:00"
	}
	if c.ActiveSchedule.End == "" {
		c.ActiveSchedule.End = "23:59"
	}
	if c.ActiveSchedule.Timezone == "" {
		c.ActiveSchedule.Timezone = "Local"
	}
	t := true
	if c.VerifyChecksum == nil {
		c.VerifyChecksum = &t
	}
	if c.EnableInitialSync == nil {
		v := true
		c.EnableInitialSync = &v
	}
	if c.Notifications == nil {
		v := true
		c.Notifications = &v
	}
}

func setDefault(v *int, def int) {
	if *v <= 0 {
		*v = def
	}
}

// Validate returns all configuration problems joined into one error.
func (c *Config) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if !c.Source.Enabled() {
		add("source host is required")
	}
	if strings.TrimSpace(c.Source.Directory) == "" {
		add("source directory is required")
	}
	if strings.TrimSpace(c.BackupDirectory) == "" {
		add("backup directory is required (processed files are archived there)")
	}
	for name, e := range map[string]Endpoint{"source": c.Source, "target": c.Target} {
		if !e.Enabled() {
			continue
		}
		if e.Port < 1 || e.Port > 65535 {
			add("%s port %d is out of range", name, e.Port)
		}
		if strings.TrimSpace(e.Username) == "" {
			add("%s username is required", name)
		}
	}
	if c.Target.Enabled() && strings.TrimSpace(c.Target.Directory) == "" {
		add("target directory is required when a target host is set")
	}
	if c.Target.Enabled() && c.Target.Addr() == c.Source.Addr() &&
		path.Clean(c.Target.Directory) == path.Clean(c.Source.Directory) {
		add("target directory must differ from the source directory on the same server")
	}
	if c.Source.Enabled() && path.Clean(c.BackupDirectory) == path.Clean(c.Source.Directory) {
		add("backup directory must differ from the source directory")
	}
	if c.PollInterval < 5 {
		add("poll interval must be at least 5 seconds")
	}
	if c.MaxParallel > 16 {
		add("max parallel transfers must be between 1 and 16")
	}
	for _, p := range append(append([]string{}, c.Include...), c.Exclude...) {
		if _, err := path.Match(p, ""); err != nil {
			add("invalid file pattern %q", p)
		}
	}
	if c.ActiveSchedule.Enabled {
		if _, err := c.ActiveSchedule.Location(); err != nil {
			add("schedule timezone: %v", err)
		}
		if _, err := parseClock(c.ActiveSchedule.Start); err != nil {
			add("schedule start: %v", err)
		}
		if _, err := parseClock(c.ActiveSchedule.End); err != nil {
			add("schedule end: %v", err)
		}
	}
	return errors.Join(errs...)
}

// Matches reports whether a file name passes the include/exclude rules.
// Matching is case-insensitive so "*.csv" also matches "REPORT.CSV".
func (c *Config) Matches(name string) bool {
	n := strings.ToLower(name)
	match := func(patterns []string) bool {
		for _, p := range patterns {
			if ok, _ := path.Match(strings.ToLower(p), n); ok {
				return true
			}
		}
		return false
	}
	return match(c.Include) && !match(c.Exclude)
}

// VerifyChecksumEnabled reports whether uploads are checksum-verified.
func (c *Config) VerifyChecksumEnabled() bool { return c.VerifyChecksum == nil || *c.VerifyChecksum }

// InitialSyncEnabled reports whether a scan runs immediately on start.
func (c *Config) InitialSyncEnabled() bool { return c.EnableInitialSync == nil || *c.EnableInitialSync }

// NotificationsEnabled reports whether desktop notifications are shown.
func (c *Config) NotificationsEnabled() bool { return c.Notifications == nil || *c.Notifications }

// Poll returns the scan interval as a duration.
func (c *Config) Poll() time.Duration { return time.Duration(c.PollInterval) * time.Second }

// Resolve makes a relative data path (log, history, known_hosts) relative to the config file's folder.
func Resolve(configPath, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(filepath.Dir(configPath), p)
}

// Location returns the schedule's time zone.
func (s Schedule) Location() (*time.Location, error) {
	if s.Timezone == "" || strings.EqualFold(s.Timezone, "local") {
		return time.Local, nil
	}
	return time.LoadLocation(s.Timezone)
}

// Active reports whether automatic scans may run at the given time.
// Windows that cross midnight (e.g. 22:00–06:00) are supported.
func (s Schedule) Active(now time.Time) bool {
	if !s.Enabled {
		return true
	}
	loc, err := s.Location()
	if err != nil {
		return true
	}
	start, err1 := parseClock(s.Start)
	end, err2 := parseClock(s.End)
	if err1 != nil || err2 != nil {
		return true
	}
	t := now.In(loc)
	m := t.Hour()*60 + t.Minute()
	if start == end {
		return true
	}
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}

func parseClock(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q is not a HH:MM time", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A config.json as written by SFTP Watchdog (capitalised keys, targetDirectory, retired fields).
const legacyJSON = `{
  "sourceSFTP": {"host": "10.0.0.5", "port": 22, "username": "dev", "password": "pw", "targetDirectory": "/files/upload"},
  "targetSFTP": {"host": "172.16.0.9", "username": "sftp", "password": "pw2", "targetDirectory": "/upload"},
  "BackupDirectory": "/files/backup",
  "IdleTimeoutSeconds": 300,
  "ReconnectInterval": 10,
  "ReconnectRetries": 5,
  "ShowBalloonTimeout": 10,
  "PollInterval": 30,
  "activeSchedule": {"enabled": false, "start": "00:00", "end": "23:59", "timezone": "Local"},
  "enableInitialSync": false
}`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMigratesWatchdogConfig(t *testing.T) {
	cfg, err := Load(writeFile(t, legacyJSON))
	if err != nil {
		t.Fatalf("legacy config should load: %v", err)
	}
	if cfg.Source.Directory != "/files/upload" || cfg.Target.Directory != "/upload" {
		t.Fatalf("directories not migrated: %+v / %+v", cfg.Source, cfg.Target)
	}
	if cfg.BackupDirectory != "/files/backup" || cfg.ReconnectRetries != 5 {
		t.Fatal("case-insensitive keys not read")
	}
	if cfg.Target.Port != 22 {
		t.Fatal("default port not applied")
	}
	if cfg.InitialSyncEnabled() {
		t.Fatal("explicit false must be kept")
	}
}

func TestSaveDropsLegacyFieldsAndIsPrivate(t *testing.T) {
	cfg, _ := Load(writeFile(t, legacyJSON))
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if strings.Contains(string(data), "targetDirectory") {
		t.Fatal("legacy key written back")
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("config must be owner-only, got %v", fi.Mode().Perm())
	}
	again, err := Load(p)
	if err != nil || again.Source.Directory != "/files/upload" {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	cfg := &Config{
		Source:          Endpoint{Host: "s", Port: 70000, Directory: "/in"},
		Target:          Endpoint{Host: "t", Username: "u"},
		BackupDirectory: "/in",
		Include:         []string{"[bad"},
		PollInterval:    1,
	}
	cfg.ApplyDefaults()
	cfg.PollInterval = 1
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"port 70000", "source username", "target directory", "backup directory must differ", "poll interval", "invalid file pattern"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestExampleIsValid(t *testing.T) {
	if err := Example().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMatches(t *testing.T) {
	cfg := &Config{Include: []string{"*.csv", "*.xml"}}
	cfg.ApplyDefaults()
	cases := map[string]bool{
		"a.csv": true, "REPORT.CSV": true, "b.xml": true, "c.txt": false,
		".hidden.csv": false, "upload.csv.part": false, "x.csv.tmp": false, "~lock.csv": false,
	}
	for name, want := range cases {
		if got := cfg.Matches(name); got != want {
			t.Errorf("Matches(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestStableSecondsDefaults(t *testing.T) {
	c := &Config{}
	c.ApplyDefaults()
	if c.StableSeconds != 10 {
		t.Fatalf("default = %d", c.StableSeconds)
	}
	c = &Config{StableSeconds: -1}
	c.ApplyDefaults()
	if c.StableSeconds != -1 {
		t.Fatal("-1 (off) must be kept")
	}
}

func TestScheduleActive(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC) }
	day := Schedule{Enabled: true, Start: "08:00", End: "17:00", Timezone: "UTC"}
	night := Schedule{Enabled: true, Start: "22:00", End: "06:00", Timezone: "UTC"}
	cases := []struct {
		s    Schedule
		t    time.Time
		want bool
	}{
		{day, at(7, 59), false}, {day, at(8, 0), true}, {day, at(16, 59), true}, {day, at(17, 0), false},
		{night, at(23, 0), true}, {night, at(3, 0), true}, {night, at(6, 0), false}, {night, at(12, 0), false},
		{Schedule{}, at(3, 0), true},
	}
	for _, c := range cases {
		if got := c.s.Active(c.t); got != c.want {
			t.Errorf("%s–%s at %s = %v, want %v", c.s.Start, c.s.End, c.t.Format("15:04"), got, c.want)
		}
	}
	// SFTP Watchdog rejected overnight windows at load time; now they validate.
	cfg := Example()
	cfg.ActiveSchedule = night
	if err := cfg.Validate(); err != nil {
		t.Fatalf("overnight window should be valid: %v", err)
	}
}

func TestScheduleTimezone(t *testing.T) {
	s := Schedule{Enabled: true, Start: "09:00", End: "10:00", Timezone: "Asia/Jakarta"}
	// 02:30 UTC is 09:30 in Jakarta (UTC+7).
	if !s.Active(time.Date(2026, 1, 1, 2, 30, 0, 0, time.UTC)) {
		t.Fatal("timezone not applied")
	}
}

func TestResolve(t *testing.T) {
	base := filepath.Join("cfg", "config.json")
	if got := Resolve(base, "history.json"); got != filepath.Join("cfg", "history.json") {
		t.Fatalf("got %s", got)
	}
	abs := filepath.Join(string(filepath.Separator), "var", "x")
	if got := Resolve(base, abs); got != abs {
		t.Fatalf("absolute path changed: %s", got)
	}
}

func TestLocalBackupValidation(t *testing.T) {
	dir := t.TempDir() // an absolute path on every platform

	// A local folder alone is a complete setup: no source server needed.
	cfg := &Config{
		Target:      Endpoint{Host: "t", Username: "u", Directory: "/in"},
		LocalBackup: LocalBackup{Enabled: true, Directory: dir},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("local-only config should be valid: %v", err)
	}
	if !cfg.LocalBackup.Keep() || !cfg.LocalBackupActive() || cfg.LocalTargetDir() != "/in" {
		t.Fatalf("defaults: %+v", cfg.LocalBackup)
	}
	cfg.LocalBackup.TargetDirectory = "/in/laptop"
	if cfg.LocalTargetDir() != "/in/laptop" {
		t.Fatal("target override ignored")
	}

	bad := *cfg
	bad.Target = Endpoint{}
	bad.LocalBackup = LocalBackup{Enabled: true, Directory: "relative/path", AfterUpload: LocalArchive, ArchiveFolder: "../outside"}
	err := bad.Validate()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{"full path", "needs a target", "subfolder inside"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}

	// Without a source server and without local backup there is nothing to do.
	none := &Config{Target: Endpoint{Host: "t", Username: "u", Directory: "/in"}}
	none.ApplyDefaults()
	if err := none.Validate(); err == nil || !strings.Contains(err.Error(), "source host is required") {
		t.Fatalf("got %v", err)
	}

	archive := *cfg
	archive.LocalBackup.AfterUpload = LocalArchive
	archive.LocalBackup.ArchiveFolder = "done/2026"
	if sub, err := archive.LocalArchiveFolder(); err != nil || sub != "done/2026" {
		t.Fatalf("archive folder = %q, %v", sub, err)
	}
	if err := archive.Validate(); err != nil {
		t.Fatal(err)
	}
}

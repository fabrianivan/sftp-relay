package setup

import (
	"net"
	"os"
	"path/filepath"
	"strings"
)

type netAddr = net.Addr

// expandHome turns "~/..." into an absolute path.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// DefaultConfigPath keeps working-directory config files from SFTP Watchdog
// working, and otherwise uses the per-user config folder
// (e.g. ~/Library/Application Support/sftp-relay/config.json on macOS).
func DefaultConfigPath() string {
	if _, err := os.Stat("config.json"); err == nil {
		if abs, err := filepath.Abs("config.json"); err == nil {
			return abs
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(dir, "sftp-relay", "config.json")
}

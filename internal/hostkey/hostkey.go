// Package hostkey verifies SSH server identities.
//
// SFTP Watchdog used ssh.InsecureIgnoreHostKey when no fingerprint was pinned,
// which allowed man-in-the-middle attacks. SFTP Relay instead pins a fingerprint
// when one is configured, and otherwise uses an OpenSSH known_hosts file with
// trust-on-first-use: the first key seen for a host is recorded, and any later
// change is refused.
package hostkey

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ErrChanged is returned when a server presents a different key than the recorded one.
var ErrChanged = errors.New("host key changed")

// ErrUnknown is returned in strict mode for hosts that are not in known_hosts.
var ErrUnknown = errors.New("unknown host key")

// Policy describes how host keys are checked.
type Policy struct {
	KnownHostsPath string
	Pinned         string // optional "SHA256:..." fingerprint
	Strict         bool   // refuse unknown hosts instead of trusting on first use

	// OnTrust is called when a new host key is recorded (trust on first use).
	OnTrust func(host, fingerprint string)
}

var fileMu sync.Mutex

// Fingerprint returns the OpenSSH SHA256 fingerprint of a key.
func Fingerprint(key ssh.PublicKey) string { return ssh.FingerprintSHA256(key) }

func normalizeFingerprint(s string) string {
	s = strings.TrimSpace(s)
	if s != "" && !strings.HasPrefix(s, "SHA256:") {
		s = "SHA256:" + s
	}
	return strings.TrimRight(s, "=")
}

// Callback returns an ssh.HostKeyCallback implementing the policy.
func (p Policy) Callback() ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		fp := Fingerprint(key)
		if p.Pinned != "" {
			if normalizeFingerprint(p.Pinned) != strings.TrimRight(fp, "=") {
				return fmt.Errorf("%w for %s: expected %s, server sent %s", ErrChanged, hostname, p.Pinned, fp)
			}
			return nil
		}
		return p.checkKnownHosts(hostname, remote, key, fp)
	}
}

func (p Policy) checkKnownHosts(hostname string, remote net.Addr, key ssh.PublicKey, fp string) error {
	if p.KnownHostsPath == "" {
		return errors.New("no known_hosts file configured")
	}
	fileMu.Lock()
	defer fileMu.Unlock()

	if err := ensureFile(p.KnownHostsPath); err != nil {
		return err
	}
	cb, err := knownhosts.New(p.KnownHostsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", p.KnownHostsPath, err)
	}
	err = cb(hostname, remote, key)
	if err == nil {
		return nil
	}
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) {
		return err
	}
	if len(ke.Want) > 0 {
		lines := make([]string, 0, len(ke.Want))
		for _, w := range ke.Want {
			lines = append(lines, fmt.Sprintf("line %d", w.Line))
		}
		return fmt.Errorf("%w for %s (now %s). This can mean someone is intercepting the connection. "+
			"If the server was legitimately reinstalled, remove %s from %s",
			ErrChanged, hostname, fp, strings.Join(lines, ", "), p.KnownHostsPath)
	}
	if p.Strict {
		return fmt.Errorf("%w %s (%s); strict host key checking is on — trust it with “Test connection” first", ErrUnknown, hostname, fp)
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	f, err := os.OpenFile(p.KnownHostsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}
	if p.OnTrust != nil {
		p.OnTrust(hostname, fp)
	}
	return nil
}

// Trust records a host key explicitly (used by "Test connection" in strict mode).
func Trust(knownHostsPath, hostname string, key ssh.PublicKey) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	if err := ensureFile(knownHostsPath); err != nil {
		return err
	}
	f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n")
	return err
}

func ensureFile(p string) error {
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

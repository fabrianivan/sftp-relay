// Package secrets stores SFTP passwords in the operating system's credential
// store (macOS Keychain, Windows Credential Manager, Linux Secret Service)
// so they never have to live in config.json.
package secrets

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Service is the name entries are stored under.
const Service = "sftp-relay"

var (
	// ErrNotFound means no secret is stored for the account.
	ErrNotFound = errors.New("secret not found")
	// ErrUnsupported means no credential store is available on this system.
	ErrUnsupported = errors.New("no system credential store available")
)

// Store is a credential store.
type Store interface {
	Get(account string) (string, error)
	Set(account, secret string) error
	Delete(account string) error
}

// Account returns the key a server's password is stored under.
func Account(user, host string, port int) string {
	return sanitize(fmt.Sprintf("%s@%s:%d", user, host, port))
}

// sanitize keeps account names safe to pass to OS tools.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '@', r == '.', r == '_', r == '-', r == ':', r == '[', r == ']':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// Source describes where a password came from (for logging, never the value).
type Source string

const (
	FromEnv      Source = "environment variable"
	FromKeychain Source = "system keychain"
	FromConfig   Source = "config file (plaintext)"
	FromNone     Source = "none"
)

// EnvVar returns the environment variable that overrides a role's password,
// e.g. SFTP_RELAY_SOURCE_PASSWORD.
func EnvVar(role, kind string) string {
	return "SFTP_RELAY_" + strings.ToUpper(role) + "_" + strings.ToUpper(kind)
}

// ResolvePassword finds an endpoint password: environment variable first,
// then the credential store, then the plaintext value from the config file.
func ResolvePassword(store Store, role, user, host string, port int, plaintext string) (string, Source) {
	if v, ok := os.LookupEnv(EnvVar(role, "password")); ok && v != "" {
		return v, FromEnv
	}
	if store != nil {
		if v, err := store.Get(Account(user, host, port)); err == nil && v != "" {
			return v, FromKeychain
		}
	}
	if plaintext != "" {
		return plaintext, FromConfig
	}
	return "", FromNone
}

// encode/decode wrap secrets so they survive tools that only handle printable text.
const encPrefix = "sftp-relay:b64:"

func encode(s string) string { return encPrefix + base64.StdEncoding.EncodeToString([]byte(s)) }

func decode(s string) string {
	s = strings.TrimRight(s, "\r\n")
	if strings.HasPrefix(s, encPrefix) {
		if b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, encPrefix)); err == nil {
			return string(b)
		}
	}
	return s
}

// Memory is an in-process Store, used in tests and as a fallback.
type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

func (s *Memory) Get(account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[account]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (s *Memory) Set(account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[account] = secret
	return nil
}

func (s *Memory) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, account)
	return nil
}

package hostkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func newKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

var addr = &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 22}

func TestTrustOnFirstUseThenDetectChange(t *testing.T) {
	kh := filepath.Join(t.TempDir(), "nested", "known_hosts")
	var trusted []string
	p := Policy{KnownHostsPath: kh, OnTrust: func(h, fp string) { trusted = append(trusted, h) }}
	cb := p.Callback()
	k1 := newKey(t)

	if err := cb("sftp.example.com:22", addr, k1); err != nil {
		t.Fatalf("first connection should be trusted: %v", err)
	}
	if len(trusted) != 1 {
		t.Fatal("OnTrust not called")
	}
	if data, _ := os.ReadFile(kh); !strings.HasPrefix(string(data), "sftp.example.com ssh-ed25519 ") {
		t.Fatalf("known_hosts = %q", data)
	}
	if err := cb("sftp.example.com:22", addr, k1); err != nil {
		t.Fatalf("same key should pass: %v", err)
	}
	if err := cb("sftp.example.com:22", addr, newKey(t)); !errors.Is(err, ErrChanged) {
		t.Fatalf("changed key must be refused, got %v", err)
	}
	if len(trusted) != 1 {
		t.Fatal("a changed key must never be trusted automatically")
	}
}

func TestNonStandardPortIsSeparateEntry(t *testing.T) {
	kh := filepath.Join(t.TempDir(), "known_hosts")
	cb := Policy{KnownHostsPath: kh}.Callback()
	if err := cb("h:2222", addr, newKey(t)); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(kh); !strings.HasPrefix(string(data), "[h]:2222 ") {
		t.Fatalf("known_hosts = %q", data)
	}
}

func TestStrictModeRefusesUnknownHosts(t *testing.T) {
	kh := filepath.Join(t.TempDir(), "known_hosts")
	k := newKey(t)
	cb := Policy{KnownHostsPath: kh, Strict: true}.Callback()
	if err := cb("h:22", addr, k); !errors.Is(err, ErrUnknown) {
		t.Fatalf("want ErrUnknown, got %v", err)
	}
	if err := Trust(kh, "h:22", k); err != nil {
		t.Fatal(err)
	}
	if err := cb("h:22", addr, k); err != nil {
		t.Fatalf("explicitly trusted key should pass: %v", err)
	}
}

func TestPinnedFingerprint(t *testing.T) {
	k := newKey(t)
	fp := Fingerprint(k)
	for _, pin := range []string{fp, strings.TrimPrefix(fp, "SHA256:"), fp + "="} {
		if err := (Policy{Pinned: pin}).Callback()("h:22", addr, k); err != nil {
			t.Errorf("pin %q: %v", pin, err)
		}
	}
	if err := (Policy{Pinned: fp}).Callback()("h:22", addr, newKey(t)); !errors.Is(err, ErrChanged) {
		t.Fatalf("pin mismatch must fail, got %v", err)
	}
}

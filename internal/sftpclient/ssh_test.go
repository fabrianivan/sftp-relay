package sftpclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/fabrianivan/sftp-relay/internal/hostkey"
	"github.com/fabrianivan/sftp-relay/internal/logging"
)

// startServer runs a real SSH+SFTP server on localhost (password "secret").
func startServer(t *testing.T) (addr string, hostKey ssh.Signer) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == "relay" && string(pw) == "secret" {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	handlers := sftp.InMemHandler()
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveConn(nc, cfg, handlers)
		}
	}()
	return ln.Addr().String(), signer
}

func serveConn(nc net.Conn, cfg *ssh.ServerConfig, h sftp.Handlers) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		nc.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			_ = nch.Reject(ssh.UnknownChannelType, "")
			continue
		}
		ch, in, err := nch.Accept()
		if err != nil {
			return
		}
		go func() {
			for req := range in {
				ok := req.Type == "subsystem" && len(req.Payload) > 4 && string(req.Payload[4:]) == "sftp"
				_ = req.Reply(ok, nil)
				if ok {
					srv := sftp.NewRequestServer(ch, h)
					_ = srv.Serve()
					ch.Close()
				}
			}
		}()
	}
}

func TestSSHDialerTOFUAndAuth(t *testing.T) {
	addr, _ := startServer(t)
	kh := filepath.Join(t.TempDir(), "known_hosts")
	var trusted atomic.Int32
	policy := hostkey.Policy{KnownHostsPath: kh, OnTrust: func(string, string) { trusted.Add(1) }}

	dial := SSHDialer(addr, Credentials{Username: "relay", Password: "secret"}, policy.Callback(), 5*time.Second)
	conn := New("source", addr, dial, time.Second, logging.Discard())
	defer conn.Close()

	err := conn.Do(context.Background(), Retry{Attempts: 1}, func(cl *sftp.Client) error {
		return cl.MkdirAll("/outbox")
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if trusted.Load() != 1 {
		t.Fatal("first connection should record the host key")
	}
	if !conn.Connected() {
		t.Fatal("should be connected")
	}

	bad := SSHDialer(addr, Credentials{Username: "relay", Password: "wrong"}, policy.Callback(), 5*time.Second)
	if _, _, err := bad(context.Background()); err == nil || !strings.Contains(err.Error(), "unable to authenticate") {
		t.Fatalf("wrong password should fail, got %v", err)
	}
}

func TestSSHDialerRefusesChangedHostKey(t *testing.T) {
	addr1, _ := startServer(t)
	kh := filepath.Join(t.TempDir(), "known_hosts")
	policy := hostkey.Policy{KnownHostsPath: kh}
	creds := Credentials{Username: "relay", Password: "secret"}

	cl, c, err := SSHDialer(addr1, creds, policy.Callback(), 5*time.Second)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cl.Close()
	c.Close()

	// A different server (new key) answering on the same host:port must be refused.
	addr2, _ := startServer(t)
	_, port1, _ := net.SplitHostPort(addr1)
	_, port2, _ := net.SplitHostPort(addr2)
	// Rewrite the recorded entry so it claims addr2 had addr1's key.
	data := readFile(t, kh)
	writeFile(t, kh, strings.Replace(data, port1, port2, 1))

	_, _, err = SSHDialer(addr2, creds, policy.Callback(), 5*time.Second)(context.Background())
	if !errors.Is(err, hostkey.ErrChanged) {
		t.Fatalf("want ErrChanged, got %v", err)
	}
}

func TestDoRetriesOnlyConnectionErrors(t *testing.T) {
	var dials atomic.Int32
	dial := func(ctx context.Context) (*sftp.Client, io.Closer, error) {
		dials.Add(1)
		return nil, nil, errors.New("connection refused")
	}
	conn := New("target", "x:22", dial, 0, logging.Discard())
	err := conn.Do(context.Background(), Retry{Attempts: 3, Base: time.Millisecond}, func(*sftp.Client) error { return nil })
	if err == nil || dials.Load() != 3 {
		t.Fatalf("want 3 dial attempts and an error, got %d / %v", dials.Load(), err)
	}
	if IsConnectionError(&sftp.StatusError{Code: 3}) { // SSH_FX_PERMISSION_DENIED
		t.Fatal("permission denied is not a connection error")
	}
	if !IsConnectionError(io.ErrUnexpectedEOF) {
		t.Fatal("EOF means the session died")
	}
}

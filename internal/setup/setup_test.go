package setup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/logging"
	"github.com/fabrianivan/sftp-relay/internal/relay"
	"github.com/fabrianivan/sftp-relay/internal/secrets"
	"github.com/fabrianivan/sftp-relay/internal/sftpclient"
)

// sshServer starts a real SSH/SFTP server on localhost accepting relay/secret.
func sshServer(t *testing.T) (host string, port int, h sftp.Handlers) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "relay" && string(pw) == "secret" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	h = sftp.InMemHandler()
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, in, err := nch.Accept()
					if err != nil {
						return
					}
					go func() {
						for req := range in {
							ok := req.Type == "subsystem"
							_ = req.Reply(ok, nil)
							if ok {
								_ = sftp.NewRequestServer(ch, h).Serve()
								ch.Close()
							}
						}
					}()
				}
			}()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port, h
}

func TestEndToEndOverSSH(t *testing.T) {
	_, srcPort, _ := sshServer(t)
	_, dstPort, _ := sshServer(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	cfg := &config.Config{
		Source:          config.Endpoint{Host: "127.0.0.1", Port: srcPort, Username: "relay", Directory: "/outbox"},
		Target:          config.Endpoint{Host: "127.0.0.1", Port: dstPort, Username: "relay", Directory: "/inbox"},
		BackupDirectory: "/outbox/archive",
		StableSeconds:   -1,
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	store := secrets.NewMemory()
	_ = store.Set(secrets.Account("relay", "127.0.0.1", srcPort), "secret") // source from keychain
	t.Setenv("SFTP_RELAY_TARGET_PASSWORD", "secret")                        // target from env

	// "Test connection" creates the target folder and probes write access.
	res, err := Test(context.Background(), cfgPath, cfg, Target, store)
	if err != nil || !res.Writable || res.Fingerprint == "" {
		t.Fatalf("target test: %+v %v", res, err)
	}

	// Seed a file through a plain client.
	log := logging.Discard()
	rt := Runtime(cfgPath, cfg, store, log)
	if err := rt.Source.Do(context.Background(), relayRetry(), func(cl *sftp.Client) error {
		if err := cl.MkdirAll("/outbox"); err != nil {
			return err
		}
		f, err := cl.Create("/outbox/hello.txt")
		if err != nil {
			return err
		}
		_, err = f.Write([]byte("hello over ssh"))
		f.Close()
		return err
	}); err != nil {
		t.Fatal(err)
	}

	hist, _ := history.Open(filepath.Join(dir, "history.json"), 10)
	eng := relay.New(relay.Options{Runtime: rt, History: hist, Logger: log})
	defer eng.Shutdown(5 * time.Second)
	sum, err := eng.RunOnce(context.Background())
	if err != nil || sum.Delivered != 1 {
		t.Fatalf("summary %+v err %v", sum, err)
	}

	// Both hosts were trusted on first use.
	kh, _ := os.ReadFile(filepath.Join(dir, "known_hosts"))
	for _, p := range []int{srcPort, dstPort} {
		if !strings.Contains(string(kh), "[127.0.0.1]:"+strconv.Itoa(p)) {
			t.Fatalf("known_hosts missing port %d:\n%s", p, kh)
		}
	}
}

func TestTestReportsBadPassword(t *testing.T) {
	_, port, _ := sshServer(t)
	cfg := config.Example()
	cfg.Source = config.Endpoint{Host: "127.0.0.1", Port: port, Username: "relay", Password: "nope", Directory: "/"}
	if _, err := Test(context.Background(), filepath.Join(t.TempDir(), "c.json"), cfg, Source, secrets.NewMemory()); err == nil {
		t.Fatal("expected authentication failure")
	}
}

func relayRetry() sftpclient.Retry { return sftpclient.Retry{Attempts: 1} }

func TestRuntimeForLocalBackupOnly(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"a.csv": "1", "b.csv": "2", "notes.tmp": "skipped by the default rules"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{
		Target:      config.Endpoint{Host: "target.example.com", Username: "relay", Directory: "/in"},
		LocalBackup: config.LocalBackup{Enabled: true, Directory: dir},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	rt := Runtime(filepath.Join(t.TempDir(), "config.json"), cfg, nil, logging.Discard())
	if rt.Source != nil || rt.Target == nil || rt.Local == nil {
		t.Fatalf("runtime = source %v, target %v, local %v", rt.Source != nil, rt.Target != nil, rt.Local != nil)
	}

	res, err := TestLocal(cfg)
	if err != nil || res.Files != 3 || res.Matching != 2 || !res.Writable {
		t.Fatalf("TestLocal = %+v, %v", res, err)
	}
	cfg.LocalBackup.Directory = filepath.Join(dir, "missing")
	if _, err := TestLocal(cfg); err == nil {
		t.Fatal("a missing folder should be reported")
	}
}

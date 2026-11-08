// Command devserver runs two throwaway in-memory SFTP servers for trying
// SFTP Relay locally — no real servers or credentials needed.
//
//	go run ./tools/devserver            # source on :2201, target on :2202
//	SFTP_RELAY_SOURCE_PASSWORD=demo SFTP_RELAY_TARGET_PASSWORD=demo \
//	  go run . -config tools/devserver/config.json
//
// Login: user "demo", password "demo". New files are dropped into the
// source's /outbox every few seconds. Everything is lost on exit.
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func main() {
	srcAddr := flag.String("source", "127.0.0.1:2201", "source server address")
	dstAddr := flag.String("target", "127.0.0.1:2202", "target server address")
	every := flag.Duration("every", 8*time.Second, "how often to drop a new file into /outbox (0 = never)")
	flag.Parse()

	serve(*srcAddr)
	serve(*dstAddr)
	log.Printf("source sftp://demo:demo@%s/outbox  target sftp://demo:demo@%s/inbox", *srcAddr, *dstAddr)

	cl := connect(*srcAddr)
	_ = cl.MkdirAll("/outbox")
	for i, size := range []int{12 << 20, 640 << 10, 48 << 10} {
		drop(cl, fmt.Sprintf("/outbox/report-%02d.csv", i+1), size)
	}
	for n := 4; *every > 0; n++ {
		time.Sleep(*every)
		drop(cl, fmt.Sprintf("/outbox/report-%02d.csv", n), (n%5+1)*512<<10)
	}
	select {}
}

func drop(cl *sftp.Client, p string, size int) {
	f, err := cl.Create(p)
	if err != nil {
		log.Printf("create %s: %v", p, err)
		return
	}
	defer f.Close()
	line := []byte("2026-01-01,demo,42.00,relay\n")
	_, _ = f.Write(bytes.Repeat(line, size/len(line)+1)[:size])
	log.Printf("dropped %s (%d KiB)", p, size>>10)
}

func serve(addr string) {
	// Deterministic host key so known_hosts stays valid across restarts.
	seed := sha256.Sum256([]byte("sftp-relay devserver " + addr))
	signer, err := ssh.NewSignerFromKey(ed25519.NewKeyFromSeed(seed[:]))
	if err != nil {
		log.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "demo" && string(pw) == "demo" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	h := sftp.InMemHandler()
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(nc, cfg, h)
		}
	}()
}

func handle(nc net.Conn, cfg *ssh.ServerConfig, h sftp.Handlers) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	go func() {
		for r := range reqs {
			_ = r.Reply(r.Type == "keepalive@openssh.com", nil)
		}
	}()
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
					_ = ch.Close()
				}
			}
		}()
	}
}

func connect(addr string) *sftp.Client {
	c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "demo",
		Auth:            []ssh.AuthMethod{ssh.Password("demo")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // local throwaway server only
	})
	if err != nil {
		log.Fatal(err)
	}
	cl, err := sftp.NewClient(c)
	if err != nil {
		log.Fatal(err)
	}
	return cl
}

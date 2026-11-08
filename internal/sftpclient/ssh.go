package sftpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Credentials used to log in.
type Credentials struct {
	Username       string
	Password       string
	PrivateKeyPath string
	Passphrase     string // for encrypted private keys
}

func (c Credentials) authMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if c.PrivateKeyPath != "" {
		pem, err := os.ReadFile(c.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key: %w", err)
		}
		var signer ssh.Signer
		if c.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(c.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(pem)
		}
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("private key %s is encrypted: set its passphrase in the keychain or environment", c.PrivateKeyPath)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if c.Password != "" {
		pw := c.Password
		methods = append(methods,
			ssh.Password(pw),
			// Many servers only offer keyboard-interactive for passwords.
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}),
		)
	}
	if len(methods) == 0 {
		return nil, errors.New("no password or private key configured")
	}
	return methods, nil
}

// SSHDialer returns a Dialer that connects over SSH and opens an SFTP subsystem.
func SSHDialer(addr string, creds Credentials, hostKey ssh.HostKeyCallback, timeout time.Duration) Dialer {
	return func(ctx context.Context) (*sftp.Client, io.Closer, error) {
		auth, err := creds.authMethods()
		if err != nil {
			return nil, nil, err
		}
		cfg := &ssh.ClientConfig{
			User:            creds.Username,
			Auth:            auth,
			HostKeyCallback: hostKey,
			Timeout:         timeout,
		}
		d := net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
		raw, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, nil, err
		}
		if deadline, ok := ctx.Deadline(); ok {
			_ = raw.SetDeadline(deadline)
		} else {
			_ = raw.SetDeadline(time.Now().Add(timeout))
		}
		conn, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
		if err != nil {
			_ = raw.Close()
			return nil, nil, err
		}
		_ = raw.SetDeadline(time.Time{})
		sshClient := ssh.NewClient(conn, chans, reqs)
		client, err := sftp.NewClient(sshClient,
			sftp.UseConcurrentWrites(true),
			sftp.UseConcurrentReads(true),
		)
		if err != nil {
			_ = sshClient.Close()
			return nil, nil, fmt.Errorf("start sftp subsystem: %w", err)
		}
		return client, sshClient, nil
	}
}

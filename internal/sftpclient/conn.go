// Package sftpclient manages one lazily (re)connected SFTP session per server.
package sftpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/sftp"

	"github.com/fabrianivan/sftp-relay/internal/logging"
)

// Dialer opens a new SFTP session. The returned Closer tears down the
// underlying transport (SSH connection) and may implement KeepAliver.
type Dialer func(ctx context.Context) (*sftp.Client, io.Closer, error)

// KeepAliver is implemented by transports that support keepalive requests.
type KeepAliver interface {
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
}

// Conn is a reconnecting SFTP session. It is safe for concurrent use:
// pkg/sftp clients multiplex concurrent requests over one session.
type Conn struct {
	Name string // "source" or "target", used in logs
	Addr string

	dial      Dialer
	log       *logging.Logger
	keepAlive time.Duration

	mu          sync.Mutex
	client      *sftp.Client
	closer      io.Closer
	connectedAt time.Time
	stopKA      chan struct{}
	dialMu      sync.Mutex
}

// New creates a Conn. keepAlive <= 0 disables keepalive requests.
func New(name, addr string, dial Dialer, keepAlive time.Duration, log *logging.Logger) *Conn {
	return &Conn{Name: name, Addr: addr, dial: dial, keepAlive: keepAlive, log: log}
}

// Connected reports whether a session is currently open.
func (c *Conn) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client != nil
}

// Client returns the open session or dials a new one.
func (c *Conn) Client(ctx context.Context) (*sftp.Client, error) {
	c.mu.Lock()
	cl := c.client
	c.mu.Unlock()
	if cl != nil {
		return cl, nil
	}

	// Serialize dials so concurrent transfers don't open several sessions.
	c.dialMu.Lock()
	defer c.dialMu.Unlock()
	c.mu.Lock()
	cl = c.client
	c.mu.Unlock()
	if cl != nil {
		return cl, nil
	}

	start := time.Now()
	cl, closer, err := c.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect %s (%s): %w", c.Name, c.Addr, err)
	}
	stop := make(chan struct{})
	c.mu.Lock()
	c.client, c.closer, c.connectedAt, c.stopKA = cl, closer, time.Now(), stop
	c.mu.Unlock()
	c.log.Infof("Connected to %s %s in %s", c.Name, c.Addr, time.Since(start).Round(time.Millisecond))

	if ka, ok := closer.(KeepAliver); ok && c.keepAlive > 0 {
		go c.keepAliveLoop(cl, ka, stop)
	}
	return cl, nil
}

func (c *Conn) keepAliveLoop(cl *sftp.Client, ka KeepAliver, stop <-chan struct{}) {
	t := time.NewTicker(c.keepAlive)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if _, _, err := ka.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				c.log.Warnf("Keepalive to %s %s failed: %v — will reconnect on next use", c.Name, c.Addr, err)
				c.Invalidate(cl)
				return
			}
		}
	}
}

// Invalidate closes the session if it is still the given client
// (called after an I/O error so the next call reconnects).
func (c *Conn) Invalidate(cl *sftp.Client) {
	c.mu.Lock()
	if c.client != cl || cl == nil {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	c.Close()
}

// Close ends the session (a later Client call reconnects).
func (c *Conn) Close() {
	c.mu.Lock()
	cl, closer, since, stop := c.client, c.closer, c.connectedAt, c.stopKA
	c.client, c.closer, c.stopKA = nil, nil, nil
	c.mu.Unlock()
	if cl == nil {
		return
	}
	if stop != nil {
		close(stop)
	}
	_ = cl.Close()
	if closer != nil {
		_ = closer.Close()
	}
	c.log.Infof("Disconnected from %s %s after %s", c.Name, c.Addr, time.Since(since).Round(time.Second))
}

// Retry describes the reconnect/backoff policy.
type Retry struct {
	Attempts int           // total attempts (>= 1)
	Base     time.Duration // first backoff; doubles each attempt
	Max      time.Duration // backoff cap
}

func (r Retry) delay(attempt int) time.Duration {
	d := r.Base << (attempt - 1)
	if r.Max > 0 && (d > r.Max || d <= 0) {
		d = r.Max
	}
	return d
}

// Do runs fn with a live client, reconnecting and retrying on connection
// errors. Errors reported by the server (permission denied, no such file…)
// are returned immediately because retrying would not help.
func (c *Conn) Do(ctx context.Context, r Retry, fn func(*sftp.Client) error) error {
	if r.Attempts < 1 {
		r.Attempts = 1
	}
	var last error
	for attempt := 1; attempt <= r.Attempts; attempt++ {
		cl, err := c.Client(ctx)
		if err == nil {
			err = fn(cl)
			if err == nil {
				return nil
			}
			if !IsConnectionError(err) {
				return err
			}
			c.Invalidate(cl)
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt < r.Attempts {
			d := r.delay(attempt)
			c.log.Warnf("%s %s: %v — retry %d/%d in %s", c.Name, c.Addr, err, attempt, r.Attempts-1, d)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
	}
	return last
}

// IsConnectionError reports whether err means the session is unusable
// (as opposed to a status error the server sent back).
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var se *sftp.StatusError
	if errors.As(err, &se) {
		return se.FxCode() == sftp.ErrSSHFxConnectionLost || se.FxCode() == sftp.ErrSSHFxNoConnection
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrExist) {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return false // the server answered (e.g. "not a directory")
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return IsConnectionError(pe.Err)
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return IsConnectionError(le.Err)
	}
	return true
}

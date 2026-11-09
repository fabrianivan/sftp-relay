// Package setup wires configuration, credentials, host-key policy and
// connections together. Both the GUI and the headless CLI use it.
package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/fabrianivan/sftp-relay/internal/audit"
	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/hostkey"
	"github.com/fabrianivan/sftp-relay/internal/logging"
	"github.com/fabrianivan/sftp-relay/internal/relay"
	"github.com/fabrianivan/sftp-relay/internal/secrets"
	"github.com/fabrianivan/sftp-relay/internal/sftpclient"
)

const dialTimeout = 20 * time.Second

// Role names used in logs, keychain lookups and environment variables.
const (
	Source = "source"
	Target = "target"
)

// Endpoint returns the endpoint config for a role.
func Endpoint(cfg *config.Config, role string) config.Endpoint {
	if role == Target {
		return cfg.Target
	}
	return cfg.Source
}

// PassphraseAccount is the keychain key for a private-key passphrase.
func PassphraseAccount(ep config.Endpoint) string {
	return secrets.Account(ep.Username, ep.Host, ep.Port) + "#key-passphrase"
}

// Credentials resolves how to log in to an endpoint without logging secrets.
func Credentials(cfgPath string, ep config.Endpoint, role string, store secrets.Store, log *logging.Logger) sftpclient.Credentials {
	pw, src := secrets.ResolvePassword(store, role, ep.Username, ep.Host, ep.Port, ep.Password)
	creds := sftpclient.Credentials{Username: ep.Username, Password: pw}
	if ep.PrivateKeyPath != "" {
		creds.PrivateKeyPath = config.Resolve(cfgPath, expandHome(ep.PrivateKeyPath))
		if v := os.Getenv(secrets.EnvVar(role, "key_passphrase")); v != "" {
			creds.Passphrase = v
		} else if store != nil {
			creds.Passphrase, _ = store.Get(PassphraseAccount(ep))
		}
	}
	if log != nil {
		switch {
		case src == secrets.FromConfig:
			log.Warnf("%s password is stored in plain text in the config file — move it to the system keychain from Settings", role)
		case src != secrets.FromNone:
			log.Debugf("%s password loaded from %s", role, src)
		case ep.PrivateKeyPath == "":
			log.Warnf("%s has no password or private key configured", role)
		}
	}
	return creds
}

// HostKeyPolicy returns the host-key policy for an endpoint.
func HostKeyPolicy(cfgPath string, cfg *config.Config, ep config.Endpoint, log *logging.Logger) hostkey.Policy {
	return hostkey.Policy{
		KnownHostsPath: config.Resolve(cfgPath, cfg.KnownHostsFile),
		Pinned:         ep.ExpectedFingerprint,
		Strict:         cfg.StrictHostKeyChecking,
		OnTrust: func(host, fp string) {
			if log != nil {
				log.Infof("Trusted new host key for %s: %s (recorded in known_hosts)", host, fp)
			}
		},
	}
}

// Runtime builds engine connections for a configuration.
func Runtime(cfgPath string, cfg *config.Config, store secrets.Store, log *logging.Logger) relay.Runtime {
	keepAlive := time.Duration(cfg.KeepAliveDuration) * time.Second
	mk := func(role string) *sftpclient.Conn {
		ep := Endpoint(cfg, role)
		dial := sftpclient.SSHDialer(ep.Addr(), Credentials(cfgPath, ep, role, store, log),
			HostKeyPolicy(cfgPath, cfg, ep, log).Callback(), dialTimeout)
		return sftpclient.New(role, ep.Addr(), dial, keepAlive, log)
	}
	rt := relay.Runtime{Config: cfg, Source: mk(Source)}
	if cfg.Target.Enabled() {
		rt.Target = mk(Target)
	}
	return rt
}

// TestResult describes a connection test.
type TestResult struct {
	Fingerprint string
	KeyType     string
	Elapsed     time.Duration
	Files       int  // entries in the directory
	Writable    bool // a probe file could be created and removed
	Unknown     bool // host key not trusted yet (strict mode)
	key         ssh.PublicKey
	hostname    string
}

// Test connects to an endpoint, checks its directory and reports the host key.
// For the target it also verifies that files can be written.
func Test(ctx context.Context, cfgPath string, cfg *config.Config, role string, store secrets.Store) (TestResult, error) {
	ep := Endpoint(cfg, role)
	var res TestResult
	if !ep.Enabled() {
		return res, errors.New("host is empty")
	}
	policy := HostKeyPolicy(cfgPath, cfg, ep, nil)
	inner := policy.Callback()
	cb := func(hostname string, remote netAddr, key ssh.PublicKey) error {
		res.Fingerprint, res.KeyType, res.key, res.hostname = hostkey.Fingerprint(key), key.Type(), key, hostname
		err := inner(hostname, remote, key)
		if errors.Is(err, hostkey.ErrUnknown) {
			res.Unknown = true
		}
		return err
	}
	start := time.Now()
	dial := sftpclient.SSHDialer(ep.Addr(), Credentials(cfgPath, ep, role, store, nil), cb, dialTimeout)
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cl, closer, err := dial(ctx)
	if err != nil {
		return res, err
	}
	defer closer.Close()
	defer cl.Close()
	res.Elapsed = time.Since(start)

	dir := ep.Directory
	entries, err := cl.ReadDir(dir)
	if err != nil {
		if role == Target && errors.Is(err, os.ErrNotExist) {
			if err := cl.MkdirAll(dir); err != nil {
				return res, fmt.Errorf("folder %s does not exist and could not be created: %w", dir, err)
			}
		} else {
			return res, fmt.Errorf("cannot read %s: %w", dir, err)
		}
	}
	res.Files = len(entries)

	probeDir := dir
	if role == Source {
		probeDir = cfg.BackupDirectory
		if err := cl.MkdirAll(probeDir); err != nil {
			return res, fmt.Errorf("cannot create backup folder %s: %w", probeDir, err)
		}
	}
	res.Writable = probeWrite(cl, probeDir) == nil
	return res, nil
}

func probeWrite(cl *sftp.Client, dir string) error {
	p := path.Join(dir, fmt.Sprintf(".sftp-relay-write-test-%d", time.Now().UnixNano()))
	f, err := cl.Create(p)
	if err != nil {
		return err
	}
	_, _ = f.Write([]byte("ok"))
	_ = f.Close()
	return cl.Remove(p)
}

// TrustTested records the host key seen during a failed strict-mode test.
func TrustTested(cfgPath string, cfg *config.Config, res TestResult) error {
	if res.key == nil {
		return errors.New("no host key to trust")
	}
	return hostkey.Trust(config.Resolve(cfgPath, cfg.KnownHostsFile), res.hostname, res.key)
}

// Controller owns the live configuration for the GUI.
type Controller struct {
	Path   string
	Store  secrets.Store
	Log    *logging.Logger
	Audit  *audit.Logger
	Engine *relay.Engine

	mu  sync.Mutex
	cfg *config.Config
}

// NewController wraps an engine and its configuration.
func NewController(cfgPath string, cfg *config.Config, store secrets.Store, log *logging.Logger, auditLog *audit.Logger, eng *relay.Engine) *Controller {
	return &Controller{Path: cfgPath, Store: store, Log: log, Audit: auditLog, Engine: eng, cfg: cfg}
}

// Config returns a copy of the current configuration.
func (c *Controller) Config() *config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := *c.cfg
	cp.Include = append([]string(nil), c.cfg.Include...)
	cp.Exclude = append([]string(nil), c.cfg.Exclude...)
	return &cp
}

// Apply validates, saves and hot-reloads a configuration. It blocks until
// running transfers finish, so call it off the UI thread.
func (c *Controller) Apply(cfg *config.Config) error {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Track config changes for audit log
	oldCfg := c.Config()
	changes := detectConfigChanges(oldCfg, cfg)

	if err := config.Save(c.Path, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
	c.Engine.Reconfigure(Runtime(c.Path, cfg, c.Store, c.Log))

	// Log config changes to audit log
	if c.Audit != nil && cfg.AuditConfigChangesEnabled() && len(changes) > 0 {
		_ = c.Audit.ConfigChanged(context.Background(), changes)
	}

	return nil
}

// detectConfigChanges returns a map of what changed between two configs.
func detectConfigChanges(old, new *config.Config) map[string]interface{} {
	changes := make(map[string]interface{})

	if old.Source.Host != new.Source.Host {
		changes["source.host"] = map[string]string{"from": old.Source.Host, "to": new.Source.Host}
	}
	if old.Target.Host != new.Target.Host {
		changes["target.host"] = map[string]string{"from": old.Target.Host, "to": new.Target.Host}
	}
	if old.PollInterval != new.PollInterval {
		changes["pollInterval"] = map[string]int{"from": old.PollInterval, "to": new.PollInterval}
	}
	if old.MaxParallel != new.MaxParallel {
		changes["maxParallel"] = map[string]int{"from": old.MaxParallel, "to": new.MaxParallel}
	}
	if old.StableSeconds != new.StableSeconds {
		changes["stableSeconds"] = map[string]int{"from": old.StableSeconds, "to": new.StableSeconds}
	}

	return changes
}

// SavePassword stores a password in the system keychain.
func (c *Controller) SavePassword(ep config.Endpoint, password string) error {
	if c.Store == nil {
		return secrets.ErrUnsupported
	}
	return c.Store.Set(secrets.Account(ep.Username, ep.Host, ep.Port), password)
}

// HasStoredPassword reports whether the keychain holds a password for ep.
func (c *Controller) HasStoredPassword(ep config.Endpoint) bool {
	if c.Store == nil || !ep.Enabled() {
		return false
	}
	v, err := c.Store.Get(secrets.Account(ep.Username, ep.Host, ep.Port))
	return err == nil && v != ""
}

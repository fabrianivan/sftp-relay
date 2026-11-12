// Command sftp-relay watches a folder on one SFTP server, relays new files to
// another, verifies them and archives the originals into dated folders.
//
// Run without flags for the desktop app, or use -headless / -once on servers.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"golang.org/x/term"

	"github.com/fabrianivan/sftp-relay/internal/audit"
	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/logging"
	"github.com/fabrianivan/sftp-relay/internal/notifier"
	"github.com/fabrianivan/sftp-relay/internal/relay"
	"github.com/fabrianivan/sftp-relay/internal/secrets"
	"github.com/fabrianivan/sftp-relay/internal/setup"
	"github.com/fabrianivan/sftp-relay/ui"
)

// version is set at build time with -ldflags "-X main.version=1.2.3".
var version = "dev"

const appID = "id.fabrianivan.sftprelay"

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "fatal: %v\n%s\n", r, debug.Stack())
			os.Exit(1)
		}
	}()
	os.Exit(run())
}

func run() int {
	var (
		configPath  = flag.String("config", "", "config file (default: ./config.json if present, else the user config folder)")
		headless    = flag.Bool("headless", false, "run continuously without the GUI (servers, services)")
		once        = flag.Bool("once", false, "scan once, process all files, then exit (exit code 1 if any file failed)")
		scanCompat  = flag.Bool("scan", false, "alias for -once (SFTP Watchdog compatibility)")
		check       = flag.Bool("check", false, "validate the config and test both connections, then exit")
		setPassword = flag.String("set-password", "", "store the password for `source|target` in the system keychain")
		showVersion = flag.Bool("version", false, "print the version and exit")
		verbose     = flag.Bool("v", false, "include debug messages in console output")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println("sftp-relay", version)
		return 0
	}
	cli := *headless || *once || *scanCompat || *check || *setPassword != ""

	path := *configPath
	if path == "" {
		path = setup.DefaultConfigPath()
	}
	cfg, cfgErr := config.Load(path)
	if errors.Is(cfgErr, fs.ErrNotExist) {
		cfg = config.Example()
		if err := config.Save(path, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "cannot create %s: %v\n", path, err)
			return 2
		}
		cfgErr = fmt.Errorf("a starter configuration was created at %s — fill in your servers", path)
		if cli {
			fmt.Fprintln(os.Stderr, cfgErr)
			return 2
		}
	} else if cfg == nil {
		fmt.Fprintf(os.Stderr, "cannot read %s: %v\n", path, cfgErr)
		return 2
	}

	if *setPassword != "" {
		return storePassword(cfg, *setPassword)
	}
	if cli && cfgErr != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration in %s:\n%v\n", path, cfgErr)
		return 2
	}

	logOpts := logging.Options{
		Path:          config.Resolve(path, cfg.LogFile),
		RetentionDays: cfg.LogRetentionDays,
		MinLevel:      logging.Info,
	}
	if cli {
		logOpts.Console = os.Stdout
	}
	if *verbose {
		logOpts.MinLevel = logging.Debug
	}
	log, err := logging.New(logOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot open log file: %v\n", err)
		return 2
	}
	defer log.Close()
	log.Infof("SFTP Relay %s starting (config %s)", version, path)

	hist, err := history.Open(config.Resolve(path, cfg.HistoryFile), cfg.HistoryLimit)
	if err != nil {
		log.Warnf("%v", err)
		if hist == nil {
			return 2
		}
	}
	store := secrets.Default()

	// Initialize audit logger
	var auditLog *audit.Logger
	mode := "gui"
	if *headless {
		mode = "headless"
	} else if *once || *scanCompat {
		mode = "once"
	}
	if cfg.AuditLogEnabled() {
		auditPath := config.Resolve(path, cfg.AuditLog.LogFile)
		auditLog, err = audit.New(audit.Options{
			LogPath:  auditPath,
			Disabled: false,
			Mode:     mode,
		})
		if err != nil {
			log.Warnf("Failed to initialize audit log: %v", err)
		} else {
			defer auditLog.Close()
			log.Infof("Audit logging enabled: %s", auditPath)
		}
	}

	if *check {
		return runCheck(path, cfg, store)
	}

	var gui fyne.App
	var notify notifier.Notifier = notifier.Noop{}
	if !cli {
		gui = fyneapp.NewWithID(appID)
		gui.SetIcon(ui.Icon())
		gui.Settings().SetTheme(ui.Theme{})
		notify = notifier.Func(func(title, msg string) {
			gui.SendNotification(fyne.NewNotification(ui.AppName+" — "+title, msg))
		})
	}

	eng := relay.New(relay.Options{
		Runtime:  setup.Runtime(path, cfg, store, log),
		History:  hist,
		Logger:   log,
		Notifier: notify,
		Auditor:  relay.NewAuditAdapter(auditLog),
	})

	// Log engine start to audit log
	if auditLog != nil {
		_ = auditLog.EngineStarted(context.Background(), map[string]interface{}{
			"version":      version,
			"mode":         mode,
			"source":       cfg.Source.Host,
			"target":       cfg.Target.Host,
			"localBackup":  cfg.LocalBackupActive(),
			"pollInterval": cfg.PollInterval,
		})
	}

	switch {
	case *once || *scanCompat:
		exitCode := runOnce(eng, log)
		if auditLog != nil {
			_ = auditLog.EngineStopped(context.Background(), "one-time scan completed")
		}
		return exitCode
	case *headless:
		exitCode := runHeadless(eng, log)
		if auditLog != nil {
			_ = auditLog.EngineStopped(context.Background(), "shutdown signal received")
		}
		return exitCode
	}

	ctl := setup.NewController(path, cfg, store, log, auditLog, eng)
	win := ui.NewWindow(gui, ctl, version, cfgErr)
	if cfgErr == nil {
		eng.Start()
	}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		fyne.Do(gui.Quit)
	}()
	win.Show()
	gui.Run()

	log.Infof("Shutting down")
	if auditLog != nil {
		_ = auditLog.EngineStopped(context.Background(), "user quit application")
	}
	eng.Shutdown(30 * time.Second)
	return 0
}

func runOnce(eng *relay.Engine, log *logging.Logger) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	sum, err := eng.RunOnce(ctx)
	eng.Shutdown(30 * time.Second)
	if err != nil {
		log.Errorf("%v", err)
		return 1
	}
	log.Infof("Done: %d relayed, %d archived, %d failed, %d still being written",
		sum.Delivered, sum.Archived, sum.Failed, sum.Waiting+sum.Deferred)
	if sum.Failed > 0 {
		return 1
	}
	return 0
}

func runHeadless(eng *relay.Engine, log *logging.Logger) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	eng.Start()
	<-ctx.Done()
	log.Infof("Signal received — finishing current transfers (up to 60s)")
	eng.Shutdown(60 * time.Second)
	return 0
}

func runCheck(path string, cfg *config.Config, store secrets.Store) int {
	code := 0
	var roles []string
	if cfg.Source.Enabled() {
		roles = append(roles, setup.Source)
	}
	if cfg.Target.Enabled() {
		roles = append(roles, setup.Target)
	}
	if cfg.LocalBackup.Enabled {
		fmt.Printf("%-6s %s ... ", "local", cfg.LocalBackup.Directory)
		res, err := setup.TestLocal(cfg)
		switch {
		case err != nil:
			fmt.Printf("FAILED\n       %v\n", err)
			code = 1
		case !res.Writable && !cfg.LocalBackup.Keep():
			fmt.Printf("FAILED\n       the folder is read-only, so files cannot be moved to %q after upload\n", cfg.LocalBackup.ArchiveFolder)
			code = 1
		default:
			fmt.Printf("OK\n       %d file(s), %d match the include/exclude rules\n", res.Files, res.Matching)
		}
	}
	for _, role := range roles {
		ep := setup.Endpoint(cfg, role)
		fmt.Printf("%-6s %s %s ... ", role, ep.Addr(), ep.Directory)
		res, err := setup.Test(context.Background(), path, cfg, role, store)
		if err != nil {
			fmt.Printf("FAILED\n       %v\n", err)
			if res.Fingerprint != "" {
				fmt.Printf("       host key %s %s\n", res.KeyType, res.Fingerprint)
			}
			code = 1
			continue
		}
		fmt.Printf("OK (%s)\n       host key %s %s\n       %d entries, writable: %v\n",
			res.Elapsed.Round(time.Millisecond), res.KeyType, res.Fingerprint, res.Files, res.Writable)
		if !res.Writable {
			code = 1
		}
	}
	return code
}

func storePassword(cfg *config.Config, role string) int {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != setup.Source && role != setup.Target {
		fmt.Fprintln(os.Stderr, "-set-password expects 'source' or 'target'")
		return 2
	}
	ep := setup.Endpoint(cfg, role)
	if !ep.Enabled() || ep.Username == "" {
		fmt.Fprintf(os.Stderr, "configure the %s host and username first\n", role)
		return 2
	}
	fmt.Fprintf(os.Stderr, "Password for %s@%s: ", ep.Username, ep.Addr())
	var pw string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		pw = string(b)
	} else {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pw = strings.TrimRight(line, "\r\n")
	}
	if pw == "" {
		fmt.Fprintln(os.Stderr, "empty password, nothing stored")
		return 1
	}
	if err := secrets.Default().Set(secrets.Account(ep.Username, ep.Host, ep.Port), pw); err != nil {
		fmt.Fprintf(os.Stderr, "could not store password: %v\nUse the %s environment variable instead.\n", err, secrets.EnvVar(role, "password"))
		return 1
	}
	fmt.Fprintf(os.Stderr, "Stored in the system keychain as %s/%s.\n", secrets.Service, secrets.Account(ep.Username, ep.Host, ep.Port))
	if ep.Password != "" {
		fmt.Fprintln(os.Stderr, "Tip: remove the plaintext \"password\" from the config file now.")
	}
	return 0
}

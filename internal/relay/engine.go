// Package relay is the core of SFTP Relay: it scans the source folder,
// copies new files to the target, verifies them, and archives the originals.
//
// Safety rules enforced here:
//   - A file is only archived after it has been fully delivered and verified.
//     If the target is unreachable the file stays in the source folder.
//   - Files still being written (size or mtime changing) are not picked up.
//   - Each file is processed by at most one worker at a time.
//   - Uploads go to a temporary name and are renamed into place, so readers
//     on the target never see partial files.
package relay

import (
	"context"
	"fmt"
	"path"
	"runtime/debug"
	"sync"
	"time"

	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/logging"
	"github.com/fabrianivan/sftp-relay/internal/notifier"
	"github.com/fabrianivan/sftp-relay/internal/sftpclient"
)

// Runtime is the configuration and connections an engine works with.
type Runtime struct {
	Config *config.Config
	Source *sftpclient.Conn
	Target *sftpclient.Conn // nil = archive-only mode
}

// Options for New.
type Options struct {
	Runtime  Runtime
	History  *history.Store
	Logger   *logging.Logger
	Notifier notifier.Notifier
	Auditor  Auditor // Audit logger for compliance tracking (optional)
	Now      func() time.Time
}

// Summary counts the outcome of one scan.
type Summary struct {
	Found     int // files picked up
	Waiting   int // files still being written
	Delivered int
	Archived  int // archive-only mode
	Failed    int
	Deferred  int // changed during upload; retried next scan
}

// Counters are today's totals for the dashboard.
type Counters struct {
	Day       string
	Delivered int
	Failed    int
	Bytes     int64
}

// Status is a snapshot for the GUI.
type Status struct {
	Running         bool
	Scanning        bool
	TargetEnabled   bool
	SourceConnected bool
	TargetConnected bool
	ScheduleActive  bool
	LastScan        time.Time
	NextScan        time.Time
	Active          []string
	Today           Counters
}

type state struct {
	Runtime
	sem chan struct{}
}

// Engine runs scans on a schedule and relays files.
type Engine struct {
	log    *logging.Logger
	hist   *history.Store
	notify notifier.Notifier
	audit  Auditor // Audit logger (optional)
	now    func() time.Time
	bus    *bus

	ctx    context.Context // lives until Shutdown; transfers use it
	cancel context.CancelFunc

	mu         sync.Mutex
	st         *state
	running    bool
	stopLoop   context.CancelFunc
	loopDone   chan struct{}
	inflight   map[string]struct{}
	ensured    map[string]bool // backup dirs known to exist
	scanning   bool
	lastScan   time.Time
	nextScan   time.Time
	today      Counters
	reconfigMu sync.Mutex

	scanMu    sync.Mutex // one scan at a time (TryLock — never blocks callers)
	tracker   *tracker   // guarded by scanMu
	transfers sync.WaitGroup
}

// New creates an engine. Call Start to begin scheduled scans.
func New(o Options) *Engine {
	if o.Logger == nil {
		o.Logger = logging.Discard()
	}
	if o.Notifier == nil {
		o.Notifier = notifier.Noop{}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.History == nil {
		o.History, _ = history.Open("", 0)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		log:      o.Logger,
		hist:     o.History,
		notify:   o.Notifier,
		audit:    o.Auditor,
		now:      o.Now,
		bus:      newBus(),
		ctx:      ctx,
		cancel:   cancel,
		st:       newState(o.Runtime),
		inflight: map[string]struct{}{},
		ensured:  map[string]bool{},
		tracker:  newTracker(),
	}
}

func newState(rt Runtime) *state {
	n := rt.Config.MaxParallel
	if n < 1 {
		n = 1
	}
	return &state{Runtime: rt, sem: make(chan struct{}, n)}
}

func (e *Engine) state() *state {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.st
}

// Subscribe streams engine events; call the returned func to stop.
func (e *Engine) Subscribe() (<-chan Event, func()) { return e.bus.subscribe() }

// History returns the transfer journal.
func (e *Engine) History() *history.Store { return e.hist }

// Running reports whether scheduled scanning is on.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// Start begins scheduled scans (no-op if already running).
func (e *Engine) Start() {
	e.mu.Lock()
	if e.running || e.ctx.Err() != nil {
		e.mu.Unlock()
		return
	}
	ctx, stop := context.WithCancel(e.ctx)
	done := make(chan struct{})
	e.running, e.stopLoop, e.loopDone = true, stop, done
	e.mu.Unlock()

	go e.loop(ctx, done)
	e.log.Infof("Scanner started (every %s)", e.state().Config.Poll())
	e.publish(Event{Kind: StateChanged, Message: "Scanner started"})
}

// Stop pauses scheduled scans. Transfers already running finish normally.
func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	stop, done := e.stopLoop, e.loopDone
	e.running = false
	e.nextScan = time.Time{}
	e.mu.Unlock()
	stop()
	<-done
	e.log.Infof("Scanner paused")
	e.publish(Event{Kind: StateChanged, Message: "Scanner paused"})
}

// Shutdown stops scanning, cancels transfers, waits for workers and closes connections.
// Interrupted uploads leave nothing behind: temporary files are removed and
// the source file stays where it was.
func (e *Engine) Shutdown(timeout time.Duration) {
	e.Stop()
	e.cancel()
	done := make(chan struct{})
	go func() { e.transfers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		e.log.Warnf("Shutdown: transfers did not stop within %s", timeout)
	}
	st := e.state()
	st.Source.Close()
	if st.Target != nil {
		st.Target.Close()
	}
	e.bus.close()
}

// Reconfigure applies a new configuration without restarting the app.
// It waits for running transfers, swaps connections and resumes scanning
// if it was running before.
func (e *Engine) Reconfigure(rt Runtime) {
	e.reconfigMu.Lock()
	defer e.reconfigMu.Unlock()

	wasRunning := e.Running()
	e.Stop()
	e.scanMu.Lock() // wait for a manual scan in progress
	e.transfers.Wait()

	e.mu.Lock()
	old := e.st
	e.st = newState(rt)
	e.ensured = map[string]bool{}
	e.mu.Unlock()
	e.tracker = newTracker()
	e.scanMu.Unlock()

	old.Source.Close()
	if old.Target != nil {
		old.Target.Close()
	}
	e.log.Infof("Configuration applied")
	e.publish(Event{Kind: StateChanged, Message: "Configuration applied"})
	if wasRunning {
		e.Start()
	}
}

// ScanNow runs a manual scan in the background. Manual scans ignore the
// active-hours schedule. If a scan is already running this is a no-op.
func (e *Engine) ScanNow() {
	go func() {
		if _, ok := e.scan(e.ctx, true); !ok {
			e.publish(Event{Kind: StateChanged, Message: "A scan is already running"})
		}
	}()
}

// RunOnce scans once and waits until every picked-up file is processed.
func (e *Engine) RunOnce(ctx context.Context) (Summary, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-e.ctx.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	b, ok := e.scan(ctx, true)
	if !ok {
		return Summary{}, fmt.Errorf("a scan is already running")
	}
	if b.err != nil {
		return b.sum, b.err
	}
	b.wg.Wait()
	return b.summary(), nil
}

// Status returns a snapshot for display.
func (e *Engine) Status() Status {
	st := e.state()
	e.mu.Lock()
	s := Status{
		Running:        e.running,
		Scanning:       e.scanning,
		TargetEnabled:  st.Target != nil,
		LastScan:       e.lastScan,
		NextScan:       e.nextScan,
		Today:          e.today,
		ScheduleActive: st.Config.ActiveSchedule.Active(e.now()),
	}
	for name := range e.inflight {
		s.Active = append(s.Active, name)
	}
	e.mu.Unlock()
	if s.Today.Day != e.now().Format("2006-01-02") {
		s.Today = Counters{Day: e.now().Format("2006-01-02")}
	}
	s.SourceConnected = st.Source.Connected()
	if st.Target != nil {
		s.TargetConnected = st.Target.Connected()
	}
	return s
}

func (e *Engine) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	st := e.state()
	poll := st.Config.Poll()
	if st.Config.InitialSyncEnabled() {
		e.scan(ctx, true)
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	e.setNext(poll)
	idle := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		e.setNext(poll)
		st = e.state()
		if !st.Config.ActiveSchedule.Active(e.now()) {
			e.log.Debugf("Outside active hours — skipping scheduled scan")
			continue
		}
		b, ok := e.scan(ctx, false)
		if !ok {
			continue
		}
		if b.found() == 0 && e.activeCount() == 0 {
			idle++
		} else {
			idle = 0
		}
		if max := st.Config.MaxIdleScans; max > 0 && idle >= max {
			idle = 0
			if st.Source.Connected() || (st.Target != nil && st.Target.Connected()) {
				e.log.Infof("No new files in %d scans — closing idle sessions (they reopen automatically)", max)
				st.Source.Close()
				if st.Target != nil {
					st.Target.Close()
				}
			}
		}
	}
}

func (e *Engine) setNext(poll time.Duration) {
	e.mu.Lock()
	e.nextScan = e.now().Add(poll)
	e.mu.Unlock()
}

func (e *Engine) activeCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.inflight)
}

// batch tracks the files dispatched by one scan.
type batch struct {
	wg  sync.WaitGroup
	mu  sync.Mutex
	sum Summary
	err error
}

func (b *batch) found() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sum.Found
}

func (b *batch) summary() Summary {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sum
}

func (b *batch) add(o outcome) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch o {
	case outDelivered:
		b.sum.Delivered++
	case outArchived:
		b.sum.Archived++
	case outFailed:
		b.sum.Failed++
	case outDeferred:
		b.sum.Deferred++
	}
}

// scan lists the source folder and dispatches ready files. It returns false
// if another scan is in progress.
func (e *Engine) scan(ctx context.Context, manual bool) (*batch, bool) {
	if !e.scanMu.TryLock() {
		e.log.Infof("Scan skipped — another scan is still running")
		return nil, false
	}
	defer e.scanMu.Unlock()

	st := e.state()
	cfg := st.Config
	b := &batch{}
	kind := "Scheduled"
	if manual {
		kind = "Manual"
	}
	e.mu.Lock()
	e.scanning = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.scanning, e.lastScan = false, e.now()
		e.mu.Unlock()
	}()
	e.publish(Event{Kind: ScanStarted, Message: kind + " scan started"})

	dir := cfg.Source.Directory
	var entries []fileInfo
	err := st.Source.Do(ctx, retryPolicy(cfg), func(cl sftpClient) error {
		list, err := cl.ReadDir(dir)
		if err != nil {
			return err
		}
		entries = entries[:0]
		for _, fi := range list {
			entries = append(entries, fi)
		}
		return nil
	})
	if err != nil {
		b.err = fmt.Errorf("list %s on source: %w", dir, err)
		e.log.Errorf("%s scan failed: %v", kind, b.err)
		e.publish(Event{Kind: ScanFinished, Message: "Scan failed: " + err.Error(), Err: true})
		return b, true
	}

	now := e.now()
	stable := time.Duration(cfg.StableSeconds) * time.Second
	seen := make(map[string]bool, len(entries))
	for _, fi := range entries {
		if !fi.Mode().IsRegular() || !cfg.Matches(fi.Name()) {
			continue
		}
		name := fi.Name()
		seen[name] = true
		if e.isInflight(name) {
			continue
		}
		if !e.tracker.ready(name, fi.Size(), fi.ModTime(), now, stable) {
			b.sum.Waiting++
			e.log.Debugf("Waiting for %s to finish writing", name)
			continue
		}
		b.sum.Found++
		e.dispatch(st, fi, b)
	}
	e.tracker.prune(seen)

	msg := fmt.Sprintf("%s scan: %d new file(s)", kind, b.sum.Found)
	if b.sum.Waiting > 0 {
		msg += fmt.Sprintf(", %d still being written", b.sum.Waiting)
	}
	if b.sum.Found > 0 || manual {
		e.log.Infof("%s in %s", msg, path.Clean(dir))
	} else {
		e.log.Debugf("%s in %s", msg, path.Clean(dir))
	}
	e.publish(Event{Kind: ScanFinished, Message: msg, Quiet: !manual && b.sum.Found == 0 && b.sum.Waiting == 0})

	if b.sum.Found > 0 {
		go func() {
			b.wg.Wait()
			e.reportBatch(st, b.summary())
		}()
	}
	return b, true
}

func (e *Engine) reportBatch(st *state, s Summary) {
	if !st.Config.NotificationsEnabled() {
		return
	}
	ok := s.Delivered + s.Archived
	switch {
	case s.Failed > 0:
		e.notify.Notify("Some files failed", fmt.Sprintf("%d relayed, %d failed — see History for details", ok, s.Failed))
	case ok > 0:
		verb := "relayed"
		if st.Target == nil {
			verb = "archived"
		}
		e.notify.Notify("Files "+verb, fmt.Sprintf("%d file(s) %s", ok, verb))
	}
}

func (e *Engine) isInflight(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.inflight[name]
	return ok
}

func (e *Engine) dispatch(st *state, fi fileInfo, b *batch) {
	name := fi.Name()
	e.mu.Lock()
	e.inflight[name] = struct{}{}
	e.mu.Unlock()
	b.wg.Add(1)
	e.transfers.Add(1)
	go func() {
		defer e.transfers.Done()
		defer b.wg.Done()
		defer func() {
			e.mu.Lock()
			delete(e.inflight, name)
			e.mu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				e.log.Errorf("Internal error while processing %s: %v\n%s", name, r, debug.Stack())
				b.add(outFailed)
			}
		}()
		select {
		case st.sem <- struct{}{}:
		case <-e.ctx.Done():
			return
		}
		defer func() { <-st.sem }()
		b.add(e.process(e.ctx, st, fi))
	}()
}

func (e *Engine) count(o outcome, bytes int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	day := e.now().Format("2006-01-02")
	if e.today.Day != day {
		e.today = Counters{Day: day}
	}
	switch o {
	case outDelivered, outArchived:
		e.today.Delivered++
		e.today.Bytes += bytes
	case outFailed:
		e.today.Failed++
	}
}

func retryPolicy(cfg *config.Config) sftpclient.Retry {
	return sftpclient.Retry{
		Attempts: cfg.ReconnectRetries + 1,
		Base:     time.Duration(cfg.ReconnectInterval) * time.Second,
		Max:      time.Minute,
	}
}

// Auditor logs audit events for compliance and forensic investigation.
type Auditor interface {
	TransferStarted(ctx context.Context, source, target *AuditEndpoint, file *AuditFile) error
	TransferCompleted(ctx context.Context, source, target *AuditEndpoint, file *AuditFile, duration time.Duration) error
	TransferFailed(ctx context.Context, source, target *AuditEndpoint, file *AuditFile, err error, duration time.Duration) error
	FileArchived(ctx context.Context, source *AuditEndpoint, file *AuditFile) error
	EngineStarted(ctx context.Context, config map[string]interface{}) error
	EngineStopped(ctx context.Context, reason string) error
}

// AuditEndpoint represents SFTP server details for audit logging.
type AuditEndpoint struct {
	Host      string
	Port      int
	Username  string
	Directory string
}

// AuditFile represents file details for audit logging.
type AuditFile struct {
	Name        string
	Size        int64
	ModTime     time.Time
	SHA256      string
	SourcePath  string
	TargetPath  string
	ArchivePath string
}

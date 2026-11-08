package relay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/logging"
	"github.com/fabrianivan/sftp-relay/internal/sftpclient"
)

// memServer is an in-memory SFTP server reachable through net.Pipe.
type memServer struct {
	h    sftp.Handlers
	down atomic.Bool
}

func newMemServer() *memServer { return &memServer{h: safeMemFS()} }

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func (m *memServer) dialer() sftpclient.Dialer {
	return func(ctx context.Context) (*sftp.Client, io.Closer, error) {
		if m.down.Load() {
			return nil, nil, errors.New("connection refused")
		}
		c1, c2 := net.Pipe()
		srv := sftp.NewRequestServer(c1, m.h)
		go func() { _ = srv.Serve() }()
		cl, err := sftp.NewClientPipe(c2, c2, sftp.UseConcurrentWrites(true))
		if err != nil {
			return nil, nil, err
		}
		return cl, closerFunc(func() error { return srv.Close() }), nil
	}
}

func (m *memServer) client(t *testing.T) *sftp.Client {
	t.Helper()
	cl, closer, err := m.dialer()(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close(); closer.Close() })
	return cl
}

func (m *memServer) put(t *testing.T, p string, data []byte) {
	t.Helper()
	cl := m.client(t)
	if err := cl.MkdirAll(path.Dir(p)); err != nil {
		t.Fatal(err)
	}
	f, err := cl.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func (m *memServer) read(t *testing.T, p string) ([]byte, error) {
	t.Helper()
	f, err := m.client(t).Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (m *memServer) list(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := m.client(t).ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

type fixture struct {
	src, dst *memServer
	cfg      *config.Config
	eng      *Engine
	hist     *history.Store
	now      time.Time
}

func newFixture(t *testing.T, withTarget bool) *fixture {
	t.Helper()
	cfg := &config.Config{
		Source:            config.Endpoint{Host: "src", Username: "u", Directory: "/out"},
		BackupDirectory:   "/archive",
		StableSeconds:     -1, // off; stability has its own tests
		MaxParallel:       3,
		ReconnectRetries:  1,
		ReconnectInterval: 1,
	}
	if withTarget {
		cfg.Target = config.Endpoint{Host: "dst", Username: "u", Directory: "/in"}
	}
	cfg.ApplyDefaults()
	cfg.ReconnectInterval = 1
	cfg.ReconnectRetries = 1

	f := &fixture{src: newMemServer(), dst: newMemServer(), cfg: cfg, now: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)}
	f.src.put(t, "/out/.keep", nil)
	hist, _ := history.Open("", 100)
	f.hist = hist
	log := logging.Discard()
	rt := Runtime{
		Config: cfg,
		Source: sftpclient.New("source", "src:22", f.src.dialer(), 0, log),
	}
	if withTarget {
		rt.Target = sftpclient.New("target", "dst:22", f.dst.dialer(), 0, log)
	}
	f.eng = New(Options{Runtime: rt, History: hist, Logger: log, Now: func() time.Time { return f.now }})
	t.Cleanup(func() { f.eng.Shutdown(5 * time.Second) })
	return f
}

func TestRelayDeliversVerifiesAndArchives(t *testing.T) {
	f := newFixture(t, true)
	payload := bytes.Repeat([]byte("sftp-relay "), 50_000) // ~550 KB, several packets
	f.src.put(t, "/out/report.csv", payload)

	sum, err := f.eng.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Delivered != 1 || sum.Failed != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	got, err := f.dst.read(t, "/in/report.csv")
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("target content mismatch (err=%v, len=%d)", err, len(got))
	}
	if _, err := f.src.read(t, "/out/report.csv"); err == nil {
		t.Fatal("source file should have been archived")
	}
	if _, err := f.src.read(t, "/archive/2026-03-04/report.csv"); err != nil {
		t.Fatalf("archived copy missing: %v", err)
	}
	for _, n := range f.dst.list(t, "/in") {
		if strings.Contains(n, "relay-part") {
			t.Fatalf("temporary file left behind: %s", n)
		}
	}
	recs := f.hist.Records()
	if len(recs) != 1 || recs[0].Status != history.Delivered || recs[0].SHA256 == "" {
		t.Fatalf("history = %+v", recs)
	}
}

func TestTargetDownKeepsFileInSource(t *testing.T) {
	f := newFixture(t, true)
	f.src.put(t, "/out/invoice.xml", []byte("<invoice/>"))
	f.dst.down.Store(true)

	sum, _ := f.eng.RunOnce(context.Background())
	if sum.Failed != 1 || sum.Delivered != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	// SFTP Watchdog archived files without delivering them when the target was down.
	if _, err := f.src.read(t, "/out/invoice.xml"); err != nil {
		t.Fatal("file must stay in the source folder when the target is unreachable")
	}
	if names := f.src.list(t, "/archive/2026-03-04"); len(names) != 0 {
		t.Fatalf("nothing should be archived, got %v", names)
	}

	f.dst.down.Store(false)
	sum, _ = f.eng.RunOnce(context.Background())
	if sum.Delivered != 1 {
		t.Fatalf("retry summary = %+v", sum)
	}
	if got, _ := f.dst.read(t, "/in/invoice.xml"); string(got) != "<invoice/>" {
		t.Fatalf("target content = %q", got)
	}
}

func TestArchiveOnlyMode(t *testing.T) {
	f := newFixture(t, false)
	f.src.put(t, "/out/a.txt", []byte("a"))
	sum, err := f.eng.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sum.Archived != 1 || sum.Failed != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	if recs := f.hist.Records(); len(recs) != 1 || recs[0].Status != history.Archived {
		t.Fatalf("history = %+v", recs)
	}
}

func TestFiltersSkipTemporaryAndHiddenFiles(t *testing.T) {
	f := newFixture(t, true)
	f.cfg.Include = []string{"*.csv"}
	f.src.put(t, "/out/data.CSV", []byte("1"))
	f.src.put(t, "/out/data.csv.part", []byte("2"))
	f.src.put(t, "/out/.hidden.csv", []byte("3"))
	f.src.put(t, "/out/notes.txt", []byte("4"))

	sum, _ := f.eng.RunOnce(context.Background())
	if sum.Delivered != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if got := f.dst.list(t, "/in"); len(got) != 1 || got[0] != "data.CSV" {
		t.Fatalf("delivered = %v", got)
	}
}

func TestBackupNameCollisionGetsSuffix(t *testing.T) {
	f := newFixture(t, false)
	f.src.put(t, "/archive/2026-03-04/same.txt", []byte("old"))
	f.src.put(t, "/out/same.txt", []byte("new"))
	if _, err := f.eng.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.src.read(t, "/archive/2026-03-04/same_1.txt"); string(got) != "new" {
		t.Fatalf("expected collision-safe archive name, got %q", got)
	}
	if got, _ := f.src.read(t, "/archive/2026-03-04/same.txt"); string(got) != "old" {
		t.Fatal("existing archive file was overwritten")
	}
}

func TestReplacesExistingTargetFile(t *testing.T) {
	f := newFixture(t, true)
	f.dst.put(t, "/in/daily.csv", []byte("yesterday"))
	f.src.put(t, "/out/daily.csv", []byte("today"))
	if sum, _ := f.eng.RunOnce(context.Background()); sum.Delivered != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if got, _ := f.dst.read(t, "/in/daily.csv"); string(got) != "today" {
		t.Fatalf("target = %q", got)
	}
}

func TestManyFilesInParallelAreDeliveredExactlyOnce(t *testing.T) {
	f := newFixture(t, true)
	const n = 25
	for i := 0; i < n; i++ {
		f.src.put(t, path.Join("/out", "f"+string(rune('a'+i))+".dat"), bytes.Repeat([]byte{byte(i)}, 40_000))
	}
	// Fire several manual scans at once: the old app could deadlock here
	// (tryLockScan leaked a goroutine holding the mutex) or upload twice.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.eng.ScanNow() }()
	}
	wg.Wait()
	sum, err := f.eng.RunOnce(context.Background())
	for err != nil && strings.Contains(err.Error(), "already running") {
		time.Sleep(20 * time.Millisecond)
		sum, err = f.eng.RunOnce(context.Background())
	}
	_ = sum
	deadline := time.Now().Add(10 * time.Second)
	for len(f.dst.list(t, "/in")) < n && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	f.eng.transfers.Wait()
	if got := len(f.dst.list(t, "/in")); got != n {
		t.Fatalf("delivered %d files, want %d", got, n)
	}
	delivered := 0
	for _, r := range f.hist.Records() {
		if r.Status == history.Delivered {
			delivered++
		}
	}
	if delivered != n {
		t.Fatalf("history shows %d deliveries, want exactly %d", delivered, n)
	}
}

func TestArchiveFailureDoesNotReupload(t *testing.T) {
	f := newFixture(t, true)
	f.src.put(t, "/blocked", []byte("a file where the archive folder should be"))
	f.cfg.BackupDirectory = "/blocked"
	f.src.put(t, "/out/x.bin", []byte("payload"))

	sum, _ := f.eng.RunOnce(context.Background())
	if sum.Failed != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if got, _ := f.dst.read(t, "/in/x.bin"); string(got) != "payload" {
		t.Fatal("file should have reached the target before archiving failed")
	}
	f.dst.put(t, "/in/x.bin", []byte("changed on target")) // detect a second upload

	f.cfg.BackupDirectory = "/archive"
	sum, _ = f.eng.RunOnce(context.Background())
	if sum.Delivered != 1 {
		t.Fatalf("retry summary = %+v", sum)
	}
	if got, _ := f.dst.read(t, "/in/x.bin"); string(got) != "changed on target" {
		t.Fatal("file was uploaded a second time")
	}
	if _, err := f.src.read(t, "/archive/2026-03-04/x.bin"); err != nil {
		t.Fatal("file should be archived on retry")
	}
}

func TestStopAndStartAreIdempotent(t *testing.T) {
	f := newFixture(t, true)
	f.cfg.PollInterval = 5
	f.eng.Start()
	f.eng.Start()
	if !f.eng.Running() {
		t.Fatal("engine should be running")
	}
	f.eng.Stop()
	f.eng.Stop()
	if f.eng.Running() {
		t.Fatal("engine should be stopped")
	}
}

func TestReconfigureSwapsTarget(t *testing.T) {
	f := newFixture(t, true)
	other := newMemServer()
	cfg := *f.cfg
	cfg.Target.Directory = "/elsewhere"
	log := logging.Discard()
	f.eng.Reconfigure(Runtime{
		Config: &cfg,
		Source: sftpclient.New("source", "src:22", f.src.dialer(), 0, log),
		Target: sftpclient.New("target", "other:22", other.dialer(), 0, log),
	})
	f.src.put(t, "/out/r.txt", []byte("r"))
	if sum, _ := f.eng.RunOnce(context.Background()); sum.Delivered != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if got, _ := other.read(t, "/elsewhere/r.txt"); string(got) != "r" {
		t.Fatal("file should go to the new target")
	}
}

func TestSourceUnreachableReportsError(t *testing.T) {
	f := newFixture(t, true)
	f.src.down.Store(true)
	if _, err := f.eng.RunOnce(context.Background()); err == nil {
		t.Fatal("expected an error when the source is down")
	}
}

func TestHumanBytesAndSpeed(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 5 << 20: "5.0 MiB"}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
	if got := Speed(1<<20, 500*time.Millisecond); got != "2.0 MiB/s" {
		t.Errorf("sub-second speed = %q (old code reported 0 B/s)", got)
	}
}

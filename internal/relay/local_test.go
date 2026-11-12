package relay

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/logging"
	"github.com/fabrianivan/sftp-relay/internal/sftpclient"
)

// newLocalFixture builds an engine that backs up a real temporary folder to
// an in-memory target, optionally next to an in-memory source server.
func newLocalFixture(t *testing.T, mode string, withSource bool) (*fixture, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Target:            config.Endpoint{Host: "dst", Username: "u", Directory: "/in"},
		LocalBackup:       config.LocalBackup{Enabled: true, Directory: dir, AfterUpload: mode, ArchiveFolder: "sent"},
		StableSeconds:     -1,
		MaxParallel:       3,
		ReconnectRetries:  1,
		ReconnectInterval: 1,
	}
	if withSource {
		cfg.Source = config.Endpoint{Host: "src", Username: "u", Directory: "/out"}
		cfg.BackupDirectory = "/archive"
	}
	cfg.ApplyDefaults()
	cfg.ReconnectInterval = 1
	cfg.ReconnectRetries = 1
	if err := cfg.Validate(); err != nil {
		t.Fatalf("fixture config: %v", err)
	}

	f := &fixture{src: newMemServer(), dst: newMemServer(), cfg: cfg, now: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)}
	hist, _ := history.Open("", 100)
	f.hist = hist
	log := logging.Discard()
	rt := Runtime{
		Config: cfg,
		Target: sftpclient.New("target", "dst:22", f.dst.dialer(), 0, log),
		Local:  sftpclient.New("local folder", dir, sftpclient.LocalDialer(dir, cfg.LocalBackup.Keep()), 0, log),
	}
	if withSource {
		f.src.put(t, "/out/.keep", nil)
		rt.Source = sftpclient.New("source", "src:22", f.src.dialer(), 0, log)
	}
	f.eng = New(Options{Runtime: rt, History: hist, Logger: log, Now: func() time.Time { return f.now }})
	t.Cleanup(func() { f.eng.Shutdown(5 * time.Second) })
	return f, dir
}

func writeLocal(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLocalBackupKeepsOriginalAndUploadsEachVersionOnce(t *testing.T) {
	f, dir := newLocalFixture(t, config.LocalKeep, false)
	writeLocal(t, dir, "ledger.csv", "v1")

	sum, err := f.eng.RunOnce(context.Background())
	if err != nil || sum.Delivered != 1 || sum.Failed != 0 {
		t.Fatalf("first run: %+v, %v", sum, err)
	}
	if got, _ := f.dst.read(t, "/in/ledger.csv"); string(got) != "v1" {
		t.Fatalf("target = %q", got)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "ledger.csv")); err != nil || string(got) != "v1" {
		t.Fatalf("the local file must stay untouched (err=%v, content=%q)", err, got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("backup mode must not add anything to the local folder: %v", entries)
	}
	recs := f.hist.Records()
	if len(recs) != 1 || recs[0].Source != FeedLocal || recs[0].Archive != "" || recs[0].Status != history.Delivered {
		t.Fatalf("history = %+v", recs)
	}

	// The same version is not uploaded again.
	f.dst.put(t, "/in/ledger.csv", []byte("changed on target"))
	if sum, err := f.eng.RunOnce(context.Background()); err != nil || sum.Found != 0 || sum.Delivered != 0 {
		t.Fatalf("second run should find nothing: %+v, %v", sum, err)
	}
	if got, _ := f.dst.read(t, "/in/ledger.csv"); string(got) != "changed on target" {
		t.Fatal("an unchanged file was uploaded a second time")
	}

	// A new version replaces the copy on the target.
	writeLocal(t, dir, "ledger.csv", "version two")
	if sum, err := f.eng.RunOnce(context.Background()); err != nil || sum.Delivered != 1 {
		t.Fatalf("changed file: %+v, %v", sum, err)
	}
	if got, _ := f.dst.read(t, "/in/ledger.csv"); string(got) != "version two" {
		t.Fatalf("target after change = %q", got)
	}
}

func TestLocalBackupForgetsFilesThatAreGone(t *testing.T) {
	f, dir := newLocalFixture(t, config.LocalKeep, false)
	writeLocal(t, dir, "a.txt", "a")
	if sum, _ := f.eng.RunOnce(context.Background()); sum.Delivered != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Deleting locally never deletes the backup…
	if got, _ := f.dst.read(t, "/in/a.txt"); string(got) != "a" {
		t.Fatal("the copy on the target must stay")
	}
	// …and if the file comes back it is backed up again.
	writeLocal(t, dir, "a.txt", "a")
	if sum, _ := f.eng.RunOnce(context.Background()); sum.Delivered != 1 {
		t.Fatalf("a returning file should be backed up again: %+v", sum)
	}
}

func TestLocalBackupArchiveModeMovesTheFile(t *testing.T) {
	f, dir := newLocalFixture(t, config.LocalArchive, false)
	writeLocal(t, dir, "export.xml", "<export/>")

	sum, err := f.eng.RunOnce(context.Background())
	if err != nil || sum.Delivered != 1 {
		t.Fatalf("summary = %+v, %v", sum, err)
	}
	if got, _ := f.dst.read(t, "/in/export.xml"); string(got) != "<export/>" {
		t.Fatalf("target = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "export.xml")); !os.IsNotExist(err) {
		t.Fatal("archive mode should move the file out of the folder")
	}
	moved := filepath.Join(dir, "sent", "2026-03-04", "export.xml")
	if got, err := os.ReadFile(moved); err != nil || string(got) != "<export/>" {
		t.Fatalf("archived copy missing at %s: %v", moved, err)
	}
	if recs := f.hist.Records(); len(recs) != 1 || recs[0].Archive != moved {
		t.Fatalf("history = %+v", recs)
	}
	// The archive subfolder is not scanned again.
	if sum, _ := f.eng.RunOnce(context.Background()); sum.Found != 0 {
		t.Fatalf("second run = %+v", sum)
	}
}

func TestLocalFolderAndSourceServerTogether(t *testing.T) {
	f, dir := newLocalFixture(t, config.LocalKeep, true)
	f.cfg.LocalBackup.TargetDirectory = "/in/from-this-computer"
	f.src.put(t, "/out/report.csv", []byte("from the server"))
	writeLocal(t, dir, "report.csv", "from this computer")

	sum, err := f.eng.RunOnce(context.Background())
	if err != nil || sum.Delivered != 2 || sum.Failed != 0 {
		t.Fatalf("summary = %+v, %v", sum, err)
	}
	if got, _ := f.dst.read(t, "/in/report.csv"); string(got) != "from the server" {
		t.Fatalf("relayed file = %q", got)
	}
	if got, _ := f.dst.read(t, "/in/from-this-computer/report.csv"); string(got) != "from this computer" {
		t.Fatalf("backed-up file = %q", got)
	}
	if _, err := f.src.read(t, "/archive/2026-03-04/report.csv"); err != nil {
		t.Fatal("the source file should be archived as usual")
	}
	if _, err := os.Stat(filepath.Join(dir, "report.csv")); err != nil {
		t.Fatal("the local file should stay")
	}
	st := f.eng.Status()
	if !st.SourceEnabled || !st.LocalEnabled || st.SourceError != "" || st.LocalError != "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestMissingLocalFolderDoesNotStopTheSource(t *testing.T) {
	f, dir := newLocalFixture(t, config.LocalKeep, true)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	f.src.put(t, "/out/still-relayed.txt", []byte("ok"))

	sum, err := f.eng.RunOnce(context.Background())
	if err == nil {
		t.Fatal("a missing local folder should be reported")
	}
	if sum.Delivered != 1 {
		t.Fatalf("the source server must still be relayed: %+v", sum)
	}
	if st := f.eng.Status(); st.LocalError == "" || st.SourceError != "" {
		t.Fatalf("status = %+v", st)
	}

	// Once the folder is back the error clears.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := f.eng.Status(); st.LocalError != "" {
		t.Fatalf("error should clear, got %q", st.LocalError)
	}
}

func TestLocalBackupWaitsWhenTargetIsDown(t *testing.T) {
	f, dir := newLocalFixture(t, config.LocalKeep, false)
	writeLocal(t, dir, "b.bin", "payload")
	f.dst.down.Store(true)
	if sum, _ := f.eng.RunOnce(context.Background()); sum.Failed != 1 || sum.Delivered != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	f.dst.down.Store(false)
	if sum, _ := f.eng.RunOnce(context.Background()); sum.Delivered != 1 {
		t.Fatalf("retry = %+v", sum)
	}
	if got, _ := f.dst.read(t, "/in/b.bin"); string(got) != "payload" {
		t.Fatalf("target = %q", got)
	}
}

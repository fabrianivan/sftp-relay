package relay

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/sftpclient"
)

type (
	sftpClient = *sftp.Client
	fileInfo   = os.FileInfo
)

type outcome int

const (
	outDelivered outcome = iota
	outArchived
	outFailed
	outDeferred
)

// errChanged means the source file changed while it was being copied.
var errChanged = errors.New("source file changed during upload")

type delivery struct {
	sha    string
	target string
	bytes  int64
}

// process delivers one file (if a target is configured) and then archives it.
func (e *Engine) process(ctx context.Context, st *state, fi os.FileInfo) outcome {
	cfg := st.Config
	name := fi.Name()
	srcPath := path.Join(cfg.Source.Directory, name)
	key := history.Key(name, fi.Size(), fi.ModTime())
	start := e.now()
	rec := history.Record{Time: start, File: name, Size: fi.Size()}

	// A scan may list a file just before another worker archives it.
	gone := false
	_ = st.Source.Do(ctx, retryPolicy(cfg), func(cl *sftp.Client) error {
		_, err := cl.Stat(srcPath)
		gone = errors.Is(err, os.ErrNotExist)
		return err
	})
	if gone {
		e.log.Debugf("%s is no longer in the source folder — skipping", name)
		return outDeferred
	}

	e.publish(Event{Kind: TransferStarted, File: name, Size: fi.Size()})

	// Audit: Log transfer started
	if e.audit != nil && cfg.AuditTransfersEnabled() {
		auditSrc := &AuditEndpoint{
			Host:      cfg.Source.Host,
			Port:      cfg.Source.Port,
			Username:  cfg.Source.Username,
			Directory: cfg.Source.Directory,
		}
		var auditTgt *AuditEndpoint
		if st.Target != nil {
			auditTgt = &AuditEndpoint{
				Host:      cfg.Target.Host,
				Port:      cfg.Target.Port,
				Username:  cfg.Target.Username,
				Directory: cfg.Target.Directory,
			}
		}
		auditFile := &AuditFile{
			Name:       name,
			Size:       fi.Size(),
			ModTime:    fi.ModTime(),
			SourcePath: srcPath,
		}
		_ = e.audit.TransferStarted(ctx, auditSrc, auditTgt, auditFile)
	}

	var d delivery
	if st.Target != nil {
		if p, ok := e.hist.PendingFor(key); ok {
			e.log.Infof("%s was already delivered at %s — archiving only", name, p.Delivered.Format(time.DateTime))
			d = delivery{sha: p.SHA256, target: p.Target, bytes: fi.Size()}
		} else {
			var err error
			d, err = e.deliver(ctx, st, srcPath, fi)
			if errors.Is(err, errChanged) {
				e.log.Infof("%s changed while uploading — it will be retried once it is stable", name)
				e.publish(Event{Kind: TransferFailed, File: name, Message: "still being written; will retry"})
				return outDeferred
			}
			if err != nil {
				return e.fail(rec, start, fmt.Errorf("upload: %w", err), "")
			}
			if err := e.hist.MarkDelivered(key, history.Pending{SHA256: d.sha, Delivered: e.now(), Target: d.target}); err != nil {
				e.log.Warnf("Could not save history: %v", err)
			}
		}
	}

	archived, err := e.archive(ctx, st, srcPath, name)
	if err != nil {
		if st.Target != nil {
			err = fmt.Errorf("delivered to target, but archiving the source failed (will retry without re-uploading): %w", err)
		} else {
			err = fmt.Errorf("archive: %w", err)
		}
		return e.fail(rec, start, err, d.target)
	}
	_ = e.hist.ClearPending(key)

	elapsed := e.now().Sub(start)
	rec.SHA256, rec.Target, rec.Archive = d.sha, d.target, archived
	rec.DurationMS = elapsed.Milliseconds()
	out := outDelivered
	rec.Status = history.Delivered
	if st.Target == nil {
		out, rec.Status = outArchived, history.Archived
		e.log.Infof("Archived %s → %s", name, archived)
	} else {
		e.log.Infof("Relayed %s (%s) in %s at %s → %s; original archived to %s",
			name, HumanBytes(fi.Size()), elapsed.Round(time.Millisecond), Speed(fi.Size(), elapsed), d.target, archived)
	}
	if err := e.hist.Add(rec); err != nil {
		e.log.Warnf("Could not save history: %v", err)
	}

	// Audit: Log successful transfer
	if e.audit != nil && cfg.AuditTransfersEnabled() {
		auditSrc := &AuditEndpoint{
			Host:      cfg.Source.Host,
			Port:      cfg.Source.Port,
			Username:  cfg.Source.Username,
			Directory: cfg.Source.Directory,
		}
		var auditTgt *AuditEndpoint
		if st.Target != nil {
			auditTgt = &AuditEndpoint{
				Host:      cfg.Target.Host,
				Port:      cfg.Target.Port,
				Username:  cfg.Target.Username,
				Directory: cfg.Target.Directory,
			}
		}
		auditFile := &AuditFile{
			Name:        name,
			Size:        fi.Size(),
			ModTime:     fi.ModTime(),
			SHA256:      d.sha,
			SourcePath:  srcPath,
			TargetPath:  d.target,
			ArchivePath: archived,
		}
		_ = e.audit.TransferCompleted(ctx, auditSrc, auditTgt, auditFile, elapsed)

		// Also log the archive operation
		if archived != "" {
			archiveFile := &AuditFile{
				Name:        name,
				Size:        fi.Size(),
				ModTime:     fi.ModTime(),
				SHA256:      d.sha,
				SourcePath:  srcPath,
				ArchivePath: archived,
			}
			_ = e.audit.FileArchived(ctx, auditSrc, archiveFile)
		}
	}

	e.count(out, fi.Size())
	e.publish(Event{Kind: TransferDone, File: name, Size: fi.Size(), Transferred: fi.Size(), Message: string(rec.Status)})
	return out
}

func (e *Engine) fail(rec history.Record, start time.Time, err error, target string) outcome {
	rec.Status = history.Failed
	rec.Error = err.Error()
	rec.Target = target
	rec.DurationMS = e.now().Sub(start).Milliseconds()
	e.log.Errorf("%s: %v (it stays in the source folder and will be retried)", rec.File, err)
	if herr := e.hist.Add(rec); herr != nil {
		e.log.Warnf("Could not save history: %v", herr)
	}

	// Audit: Log failed transfer
	st := e.state()
	cfg := st.Config
	if e.audit != nil && cfg.AuditTransfersEnabled() {
		auditSrc := &AuditEndpoint{
			Host:      cfg.Source.Host,
			Port:      cfg.Source.Port,
			Username:  cfg.Source.Username,
			Directory: cfg.Source.Directory,
		}
		var auditTgt *AuditEndpoint
		if st.Target != nil {
			auditTgt = &AuditEndpoint{
				Host:      cfg.Target.Host,
				Port:      cfg.Target.Port,
				Username:  cfg.Target.Username,
				Directory: cfg.Target.Directory,
			}
		}
		auditFile := &AuditFile{
			Name:       rec.File,
			Size:       rec.Size,
			SourcePath: path.Join(cfg.Source.Directory, rec.File),
			TargetPath: target,
		}
		duration := time.Duration(rec.DurationMS) * time.Millisecond
		_ = e.audit.TransferFailed(context.Background(), auditSrc, auditTgt, auditFile, err, duration)
	}

	e.count(outFailed, 0)
	e.publish(Event{Kind: TransferFailed, File: rec.File, Message: err.Error(), Err: true})
	return outFailed
}

// deliver uploads with retries.
func (e *Engine) deliver(ctx context.Context, st *state, srcPath string, fi os.FileInfo) (delivery, error) {
	r := retryPolicy(st.Config)
	var last error
	for attempt := 1; attempt <= r.Attempts; attempt++ {
		d, err := e.copyOnce(ctx, st, srcPath, fi)
		if err == nil {
			return d, nil
		}
		last = err
		if errors.Is(err, errChanged) || ctx.Err() != nil {
			return d, err
		}
		if attempt < r.Attempts {
			wait := time.Duration(st.Config.ReconnectInterval) * time.Second << (attempt - 1)
			if wait > time.Minute || wait <= 0 {
				wait = time.Minute
			}
			e.log.Warnf("Upload of %s failed (attempt %d/%d): %v — retrying in %s", fi.Name(), attempt, r.Attempts, err, wait)
			select {
			case <-ctx.Done():
				return d, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	return delivery{}, last
}

// copyOnce streams source → target temp file, verifies it and renames it into place.
func (e *Engine) copyOnce(ctx context.Context, st *state, srcPath string, fi os.FileInfo) (d delivery, err error) {
	cfg := st.Config
	name := fi.Name()

	srcCl, err := st.Source.Client(ctx)
	if err != nil {
		return d, err
	}
	dstCl, err := st.Target.Client(ctx)
	if err != nil {
		return d, err
	}
	// After a connection-level failure, drop whichever session died so the retry reconnects.
	defer func() {
		if err != nil && sftpclient.IsConnectionError(err) {
			if _, e1 := srcCl.Getwd(); e1 != nil {
				st.Source.Invalidate(srcCl)
			}
			if _, e2 := dstCl.Getwd(); e2 != nil {
				st.Target.Invalidate(dstCl)
			}
		}
	}()

	src, err := srcCl.Open(srcPath)
	if err != nil {
		return d, fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	dir := cfg.Target.Directory
	if _, err := dstCl.Stat(dir); err != nil {
		if err := dstCl.MkdirAll(dir); err != nil {
			return d, fmt.Errorf("create target folder %s: %w", dir, err)
		}
	}
	final := path.Join(dir, name)
	tmp := path.Join(dir, "."+name+"."+randomSuffix()+".relay-part")

	dst, err := dstCl.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return d, fmt.Errorf("create %s: %w", tmp, err)
	}
	removeTmp := func() {
		_ = dst.Close()
		_ = dstCl.Remove(tmp)
	}

	h := sha256.New()
	pr := &progressReader{
		r:     io.TeeReader(bufio.NewReaderSize(src, 1<<20), h),
		size:  fi.Size(),
		start: time.Now(),
		emit: func(done int64, speed float64) {
			e.publish(Event{Kind: TransferProgress, File: name, Size: fi.Size(), Transferred: done, Speed: speed})
		},
		ctx: ctx,
	}
	n, err := dst.ReadFrom(pr)
	if err != nil {
		removeTmp()
		return d, fmt.Errorf("copy: %w", err)
	}
	if err := dst.Close(); err != nil {
		_ = dstCl.Remove(tmp)
		return d, fmt.Errorf("finish upload: %w", err)
	}
	if n != fi.Size() {
		_ = dstCl.Remove(tmp)
		return d, errChanged
	}
	// The source must not have changed while we were reading it.
	if now, err := srcCl.Stat(srcPath); err != nil || now.Size() != fi.Size() || !now.ModTime().Equal(fi.ModTime()) {
		_ = dstCl.Remove(tmp)
		if err != nil {
			return d, fmt.Errorf("re-check source: %w", err)
		}
		return d, errChanged
	}
	sum := hex.EncodeToString(h.Sum(nil))

	if cfg.VerifyChecksumEnabled() {
		got, err := remoteSHA256(dstCl, tmp)
		if err != nil {
			_ = dstCl.Remove(tmp)
			return d, fmt.Errorf("verify upload: %w", err)
		}
		if got != sum {
			_ = dstCl.Remove(tmp)
			return d, fmt.Errorf("checksum mismatch after upload (source %s…, target %s…)", sum[:12], got[:12])
		}
	} else if info, err := dstCl.Stat(tmp); err != nil || info.Size() != fi.Size() {
		_ = dstCl.Remove(tmp)
		if err != nil {
			return d, fmt.Errorf("verify upload: %w", err)
		}
		return d, fmt.Errorf("size mismatch after upload: expected %d, got %d", fi.Size(), info.Size())
	}

	if err := publishFile(dstCl, tmp, final); err != nil {
		_ = dstCl.Remove(tmp)
		return d, err
	}
	return delivery{sha: sum, target: final, bytes: n}, nil
}

// publishFile moves the finished temp file to its final name, replacing an
// older file atomically when the server supports posix-rename.
func publishFile(cl *sftp.Client, tmp, final string) error {
	if err := cl.PosixRename(tmp, final); err == nil {
		return nil
	}
	if _, err := cl.Stat(final); err == nil {
		if err := cl.Remove(final); err != nil {
			return fmt.Errorf("replace existing %s: %w", final, err)
		}
	}
	if err := cl.Rename(tmp, final); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmp, final, err)
	}
	return nil
}

func remoteSHA256(cl *sftp.Client, p string) (string, error) {
	f, err := cl.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := f.WriteTo(h); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// archive moves the source file into <backup>/<YYYY-MM-DD>/, never overwriting.
func (e *Engine) archive(ctx context.Context, st *state, srcPath, name string) (string, error) {
	cfg := st.Config
	dir := path.Join(cfg.BackupDirectory, e.now().Format("2006-01-02"))
	var dest string
	err := st.Source.Do(ctx, retryPolicy(cfg), func(cl *sftp.Client) error {
		if !e.dirEnsured(dir) {
			if err := cl.MkdirAll(dir); err != nil {
				return fmt.Errorf("create %s: %w", dir, err)
			}
			e.markEnsured(dir)
		}
		var err error
		dest, err = uniquePath(cl, dir, name)
		if err != nil {
			return err
		}
		return cl.Rename(srcPath, dest)
	})
	return dest, err
}

func (e *Engine) dirEnsured(dir string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ensured[dir]
}

func (e *Engine) markEnsured(dir string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ensured[dir] = true
}

// uniquePath returns dir/name, or dir/name_1.ext, name_2.ext… if taken.
func uniquePath(cl *sftp.Client, dir, name string) (string, error) {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 10000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s_%d%s", stem, i, ext)
		}
		p := path.Join(dir, candidate)
		if _, err := cl.Lstat(p); errors.Is(err, os.ErrNotExist) {
			return p, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("too many files named %s in %s", name, dir)
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// progressReader reports throughput and lets cancellation stop a copy.
type progressReader struct {
	r     io.Reader
	size  int64
	done  int64
	start time.Time
	last  time.Time
	emit  func(done int64, speed float64)
	ctx   context.Context
}

// Size lets pkg/sftp use concurrent writes for the upload.
func (p *progressReader) Size() int64 { return p.size }

func (p *progressReader) Read(b []byte) (int, error) {
	if err := p.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.r.Read(b)
	p.done += int64(n)
	if now := time.Now(); now.Sub(p.last) >= 250*time.Millisecond || errors.Is(err, io.EOF) {
		p.last = now
		var speed float64
		if el := now.Sub(p.start).Seconds(); el > 0 {
			speed = float64(p.done) / el
		}
		p.emit(p.done, speed)
	}
	return n, err
}

// HumanBytes formats a byte count.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Speed formats bytes over a duration (sub-second transfers included).
func Speed(n int64, d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return HumanBytes(int64(float64(n)/d.Seconds())) + "/s"
}

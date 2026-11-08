package relay

import (
	"os"
	"sync"
	"time"

	"github.com/pkg/sftp"
)

// pkg/sftp's InMemHandler is an example implementation: its rename mutates
// file names that directory listings read without a lock, which trips the race
// detector. safeMemFS serializes renames with listings and returns snapshots,
// so the race detector checks our code rather than that example.
func safeMemFS() sftp.Handlers {
	h := sftp.InMemHandler()
	mu := &sync.Mutex{}
	l := &snapshotLister{mu: mu, inner: h.FileList.(interface {
		sftp.FileLister
		sftp.LstatFileLister
	})}
	c := &lockedCmder{mu: mu, inner: h.FileCmd.(interface {
		sftp.FileCmder
		sftp.PosixRenameFileCmder
	})}
	return sftp.Handlers{FileGet: h.FileGet, FilePut: h.FilePut, FileCmd: c, FileList: l}
}

type lockedCmder struct {
	mu    *sync.Mutex
	inner interface {
		sftp.FileCmder
		sftp.PosixRenameFileCmder
	}
}

func (c *lockedCmder) Filecmd(r *sftp.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.Filecmd(r)
}

func (c *lockedCmder) PosixRename(r *sftp.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.PosixRename(r)
}

type snapshotLister struct {
	mu    *sync.Mutex
	inner interface {
		sftp.FileLister
		sftp.LstatFileLister
	}
}

func (s *snapshotLister) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	la, err := s.inner.Filelist(r)
	return snapshot(s.mu, la), err
}

func (s *snapshotLister) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	la, err := s.inner.Lstat(r)
	return snapshot(s.mu, la), err
}

type infoCopy struct {
	name string
	size int64
	mode os.FileMode
	mod  time.Time
}

func (i infoCopy) Name() string       { return i.name }
func (i infoCopy) Size() int64        { return i.size }
func (i infoCopy) Mode() os.FileMode  { return i.mode }
func (i infoCopy) ModTime() time.Time { return i.mod }
func (i infoCopy) IsDir() bool        { return i.mode.IsDir() }
func (i infoCopy) Sys() any           { return nil }

type listerFunc func([]os.FileInfo, int64) (int, error)

func (f listerFunc) ListAt(ls []os.FileInfo, off int64) (int, error) { return f(ls, off) }

func snapshot(mu *sync.Mutex, la sftp.ListerAt) sftp.ListerAt {
	if la == nil {
		return nil
	}
	return listerFunc(func(ls []os.FileInfo, off int64) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		n, err := la.ListAt(ls, off)
		for i := 0; i < n; i++ {
			fi := ls[i]
			ls[i] = infoCopy{fi.Name(), fi.Size(), fi.Mode(), fi.ModTime()}
		}
		return n, err
	})
}

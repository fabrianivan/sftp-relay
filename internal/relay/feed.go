package relay

import (
	"path/filepath"

	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/sftpclient"
)

// Feed names, used in events and history records.
const (
	FeedSource = "source" // the source SFTP server
	FeedLocal  = "local"  // a folder on this computer
)

// localPrefix namespaces local files inside the engine and the delivery
// journal, so both feeds can hold a file of the same name.
const localPrefix = FeedLocal + ":"

// feed is one folder the engine collects files from. Its settings are read
// from the configuration on every use, so it never works with stale values.
type feed struct {
	id   string
	conn *sftpclient.Conn
	cfg  *config.Config
}

// feedsFor lists the folders a runtime collects from. A local folder is only
// collected when there is a target to upload to.
func feedsFor(rt Runtime) []*feed {
	var feeds []*feed
	if rt.Source != nil {
		feeds = append(feeds, &feed{id: FeedSource, conn: rt.Source, cfg: rt.Config})
	}
	if rt.Local != nil && rt.Target != nil {
		feeds = append(feeds, &feed{id: FeedLocal, conn: rt.Local, cfg: rt.Config})
	}
	return feeds
}

func (f *feed) local() bool { return f.id == FeedLocal }

// label names the feed in messages.
func (f *feed) label() string {
	if f.local() {
		return "local folder"
	}
	return "source"
}

// dir is the folder to list, as the connection sees it. The local folder is
// the root of its own connection, so every path inside it is relative.
func (f *feed) dir() string {
	if f.local() {
		return "."
	}
	return f.cfg.Source.Directory
}

// shown is the same folder as the user knows it.
func (f *feed) shown() string {
	if f.local() {
		return f.cfg.LocalBackup.Directory
	}
	return f.cfg.Source.Directory
}

// display turns a path on the connection into one the user recognises.
func (f *feed) display(p string) string {
	if f.local() {
		return filepath.Join(f.cfg.LocalBackup.Directory, filepath.FromSlash(p))
	}
	return p
}

// targetDir is the folder on the target server that receives this feed's files.
func (f *feed) targetDir() string {
	if f.local() {
		return f.cfg.LocalTargetDir()
	}
	return f.cfg.Target.Directory
}

// keep reports whether delivered files stay where they are (local backup)
// instead of being moved to an archive folder.
func (f *feed) keep() bool { return f.local() && f.cfg.LocalBackup.Keep() }

// archiveRoot is the folder that receives originals after delivery.
func (f *feed) archiveRoot() string {
	if f.local() {
		if sub, err := f.cfg.LocalArchiveFolder(); err == nil {
			return sub
		}
		return "archive"
	}
	return f.cfg.BackupDirectory
}

// ref names a file inside the engine. Source files keep their plain name.
func (f *feed) ref(name string) string {
	if f.local() {
		return localPrefix + name
	}
	return name
}

// key identifies one version of a file in the delivery journal.
func (f *feed) key(fi fileInfo) string {
	return history.Key(f.ref(fi.Name()), fi.Size(), fi.ModTime())
}

// retry is the reconnect policy. A local folder is either there or it is
// not, so waiting and trying again would only stall the scan.
func (f *feed) retry() sftpclient.Retry {
	if f.local() {
		return sftpclient.Retry{Attempts: 1}
	}
	return retryPolicy(f.cfg)
}

func (f *feed) auditEndpoint() *AuditEndpoint {
	if f.local() {
		return &AuditEndpoint{Host: "localhost", Directory: f.cfg.LocalBackup.Directory}
	}
	s := f.cfg.Source
	return &AuditEndpoint{Host: s.Host, Port: s.Port, Username: s.Username, Directory: s.Directory}
}

// auditTarget describes where this feed's files are delivered.
func (f *feed) auditTarget(st *state) *AuditEndpoint {
	if st.Target == nil {
		return nil
	}
	t := f.cfg.Target
	return &AuditEndpoint{Host: t.Host, Port: t.Port, Username: t.Username, Directory: f.targetDir()}
}

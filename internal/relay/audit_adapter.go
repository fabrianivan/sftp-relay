package relay

import (
	"context"
	"time"

	"github.com/fabrianivan/sftp-relay/internal/audit"
)

// AuditAdapter adapts the audit.Logger to the Auditor interface.
type AuditAdapter struct {
	logger *audit.Logger
}

// NewAuditAdapter creates an adapter for the audit logger.
func NewAuditAdapter(logger *audit.Logger) *AuditAdapter {
	if logger == nil {
		return nil
	}
	return &AuditAdapter{logger: logger}
}

func (a *AuditAdapter) TransferStarted(ctx context.Context, source, target *AuditEndpoint, file *AuditFile) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.TransferStarted(ctx, toAuditEndpoint(source), toAuditEndpoint(target), toAuditFile(file))
}

func (a *AuditAdapter) TransferCompleted(ctx context.Context, source, target *AuditEndpoint, file *AuditFile, duration time.Duration) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.TransferCompleted(ctx, toAuditEndpoint(source), toAuditEndpoint(target), toAuditFile(file), duration)
}

func (a *AuditAdapter) TransferFailed(ctx context.Context, source, target *AuditEndpoint, file *AuditFile, err error, duration time.Duration) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.TransferFailed(ctx, toAuditEndpoint(source), toAuditEndpoint(target), toAuditFile(file), err, duration)
}

func (a *AuditAdapter) FileArchived(ctx context.Context, source *AuditEndpoint, file *AuditFile) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.FileArchived(ctx, toAuditEndpoint(source), toAuditFile(file))
}

func (a *AuditAdapter) EngineStarted(ctx context.Context, config map[string]interface{}) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.EngineStarted(ctx, config)
}

func (a *AuditAdapter) EngineStopped(ctx context.Context, reason string) error {
	if a == nil || a.logger == nil {
		return nil
	}
	return a.logger.EngineStopped(ctx, reason)
}

func toAuditEndpoint(e *AuditEndpoint) *audit.Endpoint {
	if e == nil {
		return nil
	}
	return &audit.Endpoint{
		Host:      e.Host,
		Port:      e.Port,
		Username:  e.Username,
		Directory: e.Directory,
	}
}

func toAuditFile(f *AuditFile) *audit.FileInfo {
	if f == nil {
		return nil
	}
	return &audit.FileInfo{
		Name:        f.Name,
		Size:        f.Size,
		ModTime:     f.ModTime,
		SHA256:      f.SHA256,
		SourcePath:  f.SourcePath,
		TargetPath:  f.TargetPath,
		ArchivePath: f.ArchivePath,
	}
}

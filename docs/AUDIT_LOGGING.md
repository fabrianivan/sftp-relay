# Audit Logging

SFTP Relay includes comprehensive audit logging to track all file transfers, configuration changes, and system events for compliance and forensic investigation.

## What Gets Logged

### Transfer Events
- **TRANSFER_STARTED** - When a file transfer begins
- **TRANSFER_COMPLETED** - Successful file transfer with duration and speed
- **TRANSFER_FAILED** - Failed transfer attempt with error details
- **FILE_ARCHIVED** - File moved to archive directory

### System Events
- **ENGINE_STARTED** - Relay engine initialization
- **ENGINE_STOPPED** - Relay engine shutdown
- **SCAN_STARTED** - Automatic or manual scan initiated
- **SCAN_COMPLETED** - Scan finished

### Connection Events
- **CONNECTION_SUCCESS** - Successful SFTP connection
- **CONNECTION_FAILED** - Failed connection attempt
- **AUTH_SUCCESS** - Successful authentication
- **AUTH_FAILURE** - Authentication failure

### Configuration Events
- **CONFIG_CHANGED** - Configuration updated (tracks what changed)

## Event Structure

Each audit event contains:
- **Unique ID** - Event identifier
- **Timestamp** - UTC timestamp
- **Event Type** - Category of event
- **Status** - SUCCESS, FAILED, or STARTED
- **Actor** - Who performed the action (user, hostname, PID, mode)
- **Source/Target** - SFTP server details
- **File Info** - Name, size, SHA-256 checksum, paths
- **Duration** - How long the operation took
- **Error** - Error message if failed
- **Details** - Additional context (e.g., bytes per second)

## Configuration

Add to your `config.json`:

```json
{
  "auditLog": {
    "enabled": true,
    "logFile": "logs/audit.log",
    "retentionDays": 90
  }
}
```

## Querying Audit Logs

The audit package provides query capabilities:

```go
import "github.com/fabrianivan/sftp-relay/internal/audit"

reader := audit.NewReader("logs/audit.log")

// Get all events
events, _ := reader.All()

// Get recent events
recent, _ := reader.Recent(100)

// Get transfer history for a file
history, _ := reader.TransferHistory("report.csv")

// Get failed transfers in time range
failed, _ := reader.FailedTransfers(startTime, endTime)

// Export to JSON
file, _ := os.Create("export.json")
reader.Export(file, audit.Filter{
    EventTypes: []audit.EventType{audit.EventTransferCompleted},
    Limit: 1000,
})
```

## Example Log Entry

```json
{
  "id": "1731167520123456789-12345",
  "timestamp": "2026-11-09T10:25:20.123Z",
  "event_type": "TRANSFER_COMPLETED",
  "status": "SUCCESS",
  "actor": {
    "type": "system",
    "user": "sftprelay",
    "hostname": "relay-server-01",
    "pid": 12345,
    "mode": "headless"
  },
  "source": {
    "host": "source.example.com",
    "port": 22,
    "username": "ftpuser",
    "directory": "/outbox"
  },
  "target": {
    "host": "target.example.com",
    "port": 22,
    "username": "ftpuser",
    "directory": "/inbox"
  },
  "file": {
    "name": "report.csv",
    "size": 102400,
    "mod_time": "2026-11-09T10:20:00Z",
    "sha256": "abc123def456...",
    "source_path": "/outbox/report.csv",
    "target_path": "/inbox/report.csv",
    "archive_path": "/outbox/archive/2026-11-09/report.csv"
  },
  "duration_ms": 1250,
  "details": {
    "bytes_per_second": 81920
  }
}
```

## Security

- Log files created with **0600 permissions** (owner read/write only)
- No passwords or secrets logged
- SHA-256 checksums for file integrity verification
- Append-only operation prevents tampering
- Thread-safe concurrent logging

## Compliance Use Cases

1. **Prove Transfers** - Verify files were successfully transferred
2. **Track Failures** - Identify patterns in transfer failures
3. **Performance Analysis** - Measure transfer speeds and durations
4. **Security Audits** - Review authentication and connection attempts
5. **Forensic Investigation** - Trace file movement through the system
6. **Change Management** - Track configuration changes over time

## Log Rotation

Audit logs can grow large over time. Implement rotation using:

**Linux/macOS (logrotate)**:
```
/var/log/sftp-relay/audit.log {
    daily
    rotate 90
    compress
    delaycompress
    missingok
    notifempty
    create 0600 sftprelay sftprelay
}
```

**Windows (PowerShell scheduled task)**:
```powershell
$logPath = "C:\sftp-relay\logs\audit.log"
$archivePath = "C:\sftp-relay\logs\audit-$(Get-Date -Format 'yyyy-MM-dd').log"
if (Test-Path $logPath) {
    Move-Item $logPath $archivePath
    Compress-Archive $archivePath "$archivePath.zip"
    Remove-Item $archivePath
}
```

## Performance

- **Overhead**: ~0.5-1ms per logged event
- **Disk usage**: ~200 bytes per event
- **Memory**: Minimal (events written immediately)
- **Concurrency**: Thread-safe with mutex protection

## Future Enhancements

Planned features:
- Real-time event streaming to SIEM systems
- Built-in log rotation and retention management
- Audit log viewer in UI
- Export to CSV for spreadsheet analysis
- Compliance report generation
- Log encryption at rest

## API Reference

See package documentation:
```bash
go doc github.com/fabrianivan/sftp-relay/internal/audit
```

# SFTP Uploader

Simple SFTP file mover for Windows: watch a source SFTP folder, copy new files to a target SFTP, then move the originals to a dated backup directory. Includes tray controls, scheduling, reconnect/keepalive, and daily log rotation.

## Features
- Scheduled or manual scans; tray menu for Scan Now / Show Logs / Exit.
- Hash-based dedupe via `uploaded.json` to skip already-processed files.
- Automatic reconnect/keepalive with idle disconnects to save resources.
- Date-based backup subfolders on the source SFTP after successful uploads.
- Daily log rotation with retention cleanup.

## Configuration
Create `config.json` beside the binary (or pass `-config path`):
```json
{
  "sourceSFTP": {
    "host": "source.example.com",
    "port": 22,
    "username": "user",
    "password": "pass",
    "targetDirectory": "/source/upload",
    "expectedFingerprint": "SHA256:..."
  },
  "targetSFTP": {
    "host": "target.example.com",
    "port": 22,
    "username": "user",
    "password": "pass",
    "targetDirectory": "/target/upload",
    "expectedFingerprint": "SHA256:..."
  },
  "backupDirectory": "/source/backup",
  "idleTimeoutSeconds": 300,
  "reconnectInterval": 10,
  "reconnectRetries": 5,
  "logFile": "sftp_uploader.log",
  "logRetentionDays": 7,
  "showBalloonTimeout": 10,
  "pollInterval": 30,
  "activeSchedule": {
    "enabled": true,
    "start": "05:00",
    "end": "23:45",
    "timezone": "Local"
  },
  "keepAliveDuration": 90,
  "enableInitialSync": true,
  "maxIdleScans": 10
}
```

Key notes:
- `activeSchedule.enabled` false means always-on scanning.
- If `targetSFTP.host` is empty, files are only moved to backup.
- `uploaded.json` is created automatically to track processed hashes.

## Usage
Build for Windows (tray mode):
```bat
SET GOOS=windows
SET GOARCH=amd64
go build -ldflags "-H=windowsgui" -o sftpwatchdog.exe
```

Build for macOS (tray mode):
```bash
# arm64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o sftpwatchdog-macos-arm64
# amd64 (Intel Macs)
GOOS=darwin GOARCH=amd64 go build -o sftpwatchdog-macos-amd64
```

Run normally (tray hidden by default):
```bat
sftpwatchdog.exe
```

macOS run:
```bash
./sftpwatchdog-macos-arm64
```

Single scan and exit:
```bat
sftpwatchdog.exe --scan
```

Show console/log output (useful for debugging):
```bat
go build -o sftpwatchdog.exe
sftpwatchdog.exe
```

## GitHub Actions (auto build + release)
- Every push/PR runs `go test` and `go build` on Windows.
- Tagging a release (e.g., `v1.2.3`) creates a GitHub Release and attaches the Windows build (`sftpwatchdog.exe`) for auto-update distribution. Push a new tag to publish an updated binary.

## Development
- Go 1.22+
- Run `gofmt ./...` before committing.
- The app stores logs with daily rotation and keeps at most `logRetentionDays`.

# Running Session Sync Without the Daemon

`shan sessions sync` uploads local session JSON to Shannon Cloud once. When the daemon is running, it syncs automatically (60s after startup, then every 24h). When the daemon is off, schedule it externally so sync still happens.

`shan schedule create` is meant for scheduling agent prompts and does not run arbitrary commands, so install a system scheduler entry by hand.

## macOS — launchd

Create `~/Library/LaunchAgents/com.shannon.sessions-sync.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.shannon.sessions-sync</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/shan</string>
    <string>sessions</string>
    <string>sync</string>
  </array>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key><integer>3</integer>
    <key>Minute</key><integer>30</integer>
  </dict>
  <key>StandardOutPath</key>
  <string>/tmp/shannon-sessions-sync.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/shannon-sessions-sync.err</string>
</dict>
</plist>
```

Adjust `/usr/local/bin/shan` to your install path (`which shan` to find it). Load:

```bash
launchctl load ~/Library/LaunchAgents/com.shannon.sessions-sync.plist
```

Unload (e.g. when starting the daemon and letting it handle sync):

```bash
launchctl unload ~/Library/LaunchAgents/com.shannon.sessions-sync.plist
```

## Linux — cron

```
30 3 * * * /usr/local/bin/shan sessions sync
```

Add via `crontab -e`.

## State files

| File | Purpose |
|---|---|
| `~/.shannon/sync_marker.json` | High-watermark + per-session retry bookkeeping. `cat` to triage; failed sessions show their reason and byte size. |
| `~/.shannon/sync.lock` | flock for serialization across daemon + CLI calls. **Never delete.** |
| `~/.shannon/sync_outbox/` | Only present when `sync.dry_run: true`. Contains JSON batches that would have been uploaded. |

## Verifying

```bash
# Dry run — see what would be sent without uploading
yq -i '.sync.dry_run = true' ~/.shannon/config.yaml
shan sessions sync
ls ~/.shannon/sync_outbox/

# Then flip dry_run back to false for real uploads
```

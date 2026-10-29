---
name: logs
description: View and filter Unreal Engine logs captured by ue5 server
argument-hint: "[--level error] [--category LogCompile] [--lines 50]"
allowed-tools:
  - Bash
  - Read
---

# View UE5 Logs

Query captured editor logs from the ue5 server daemon with powerful filtering.

## Arguments

- `--lines N` or `-n N`: Number of recent lines (default: 100)
- `--level LEVEL` or `-l LEVEL`: Filter by level: error, warning, info
- `--category CAT` or `-c CAT`: Filter by UE log category (e.g., LogCompile, LogInit, LogBlueprint)
- `--pattern REGEX`: Filter by regex pattern
- `--since DURATION`: Only lines after this time (e.g., "5m", "1h", or RFC3339 timestamp)
- `--follow` or `-f`: Stream new log lines in real-time (like tail -f)
- `--json`: Output as structured JSON

## Common Queries

### Recent errors
```bash
ue5 server logs --level error --lines 50
```

### Recent warnings
```bash
ue5 server logs --level warning --lines 50
```

### Compilation-related logs
```bash
ue5 server logs --category LogCompile --lines 100
```

### Search for a specific class/keyword
```bash
ue5 server logs --pattern "MyCharacter" --lines 200
```

### Logs from the last 5 minutes
```bash
ue5 server logs --since 5m --lines 200
```

### Structured JSON output (for programmatic analysis)
```bash
ue5 server logs --level error --json
```

### Stream new logs in real-time
```bash
ue5 server logs --follow --level error
```

### All logs (full session)
```bash
ue5 server logs --lines 0
```

## Process

1. Run the appropriate `ue5 server logs` command based on what the user needs
2. Analyze the output for patterns, errors, or requested information
3. If errors found, correlate with code using file paths and line numbers in the output
4. Provide a summary of findings to the user

## On-Disk Log Locations

If the daemon isn't running or you need raw log files, they live under `~/.ue5/logs/<hash>/` where `<hash>` is the first 12 hex chars of `SHA-256(project_path)`:

- **Editor log**: `~/.ue5/logs/<hash>/editor.log` — captured editor stdout/stderr
- **Build log**: `~/.ue5/logs/<hash>/build.log` — raw UBT compiler output (compile errors, linker failures)

To read a log file directly when the daemon is down:
```bash
cat ~/.ue5/logs/$(echo -n "/path/to/YourProject.uproject" | shasum -a 256 | cut -c1-12)/build.log
```

## Important Notes

- Logs are only captured for editor instances started via `ue5 server run` or `/uem:start`
- Use `--json` flag for machine-readable output when doing programmatic analysis
- The `--since` flag accepts Go duration strings (5m, 1h, 30s) or RFC3339 timestamps
- Log categories match UE5 log categories (LogInit, LogCompile, LogBlueprint, LogTemp, etc.)
- Previous session logs are preserved until the next `ue5 server run` for the same project

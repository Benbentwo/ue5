# UE5 CLI
## Unreal Engine 5 Command Line Interface
This is a command line interface for Unreal Engine 5, designed to simplify the process of building and packaging projects. It provides a set of commands that can be used to automate common tasks, such as building, packaging, and launching projects.

This is very similar to Adam Rehn's [ue4 cli](https://docs.adamrehn.com/ue4cli/overview/introduction-to-ue4cli/), but built with GO as to avoid Python dependencies and to provide a more robust solution.

## Installation
Prerequisites: Go 1.24+.

From the repo root:
```console
go install .
```

This installs the `ue5` binary to `$(go env GOPATH)/bin` (or `$(go env GOBIN)` if set). Make sure that directory is on your `PATH`.

### Install from GitHub release (no repo clone)
1) Go to the project GitHub Releases page and download the asset for your OS/arch.
2) Extract it (if it is a .zip or .tar.gz).
3) Make it executable and move it onto your PATH:
```console
chmod +x ue5
mv ue5 /usr/local/bin/ue5
```

## Usage
```console
UE5 CLI is a command line tool to help build and package Unreal Engine 5 projects.

Usage:
  ue5 [flags]
  ue5 [command]

Available Commands:
  build       Build your Project
  clean       Removes cache and intermediate files from the project
  completion  Generate the autocompletion script for the specified shell
  gen         Generate project files for your Unreal Engine Project
  help        Help about any command
  package     Package your Unreal Engine project for shipping
  run         Start the Unreal Editor for the current project
  server      Manage the UE5 editor server daemon
  upgrade     Upgrade ue5 CLI to the latest version
  version     Print the version

Flags:
  -d, --debug            Enable debug logging
  -h, --help             help for ue5
  -p, --project string   Path to the project directory (default: current directory)

Use "ue5 [command] --help" for more information about a command.
```

## How it works
This CLI looks at your current directory and searches for a `.uproject` file. If it finds one, it will use that as the project to run commands on. 
This can be overridden by using the `-p` or `--project` flag to specify a different project directory.

This CLI then runs the same commands that you would run but auto calculates the paths to the engine based on your UProject version and the Unreal Engines installed via your Epic Games Launcher manifests.

Thus with multiple versions of Unreal Engine installed, you can run commands on any project without having to specify the engine version or path.

## Commands

### `build`
Build your Unreal Engine project. Defaults to building the editor target in Development configuration.

```bash
ue5 build
ue5 build --target MyProjectEditor --state Development
ue5 build -t MyProjectGame -s Shipping
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--target` | `-t` | `<ProjectName>Editor` | Build target (e.g. `MyProjectEditor`) |
| `--state` | `-s` | `Development` | Build configuration (e.g. `Development`, `Shipping`) |

### `clean`
Remove cached and intermediate build artifacts from the project directory. Cleans `DerivedDataCache`, `Intermediate`, `Binaries`, `Build`, and `dist` by default.

```bash
ue5 clean         # Clean standard directories
ue5 clean --all   # Also clean Intermediate and Binaries inside every plugin
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--all` | `-a` | `false` | Also clean `Intermediate` and `Binaries` from all plugins in the `Plugins/` directory |

### `gen`
Generate project files. Equivalent to right-clicking the `.uproject` file and selecting **Generate Visual Studio project files** (Windows) or **Generate Xcode project files** (macOS).

```bash
ue5 gen
```

### `package`
Package the project for distribution. Runs build, cook, and stage steps to produce a distributable build.

```bash
ue5 package
ue5 package --state Shipping --output dist/win64
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--state` | `-s` | `Development` | Build configuration (e.g. `Development`, `Shipping`) |
| `--output` | `-o` | `dist` | Output directory for the packaged build |

### `run`
Open the Unreal Editor for the current project.

```bash
ue5 run
```

### `upgrade`
Check for and optionally install a newer version of the `ue5` CLI.

```bash
ue5 upgrade           # Interactive upgrade
ue5 upgrade --check   # Check for updates without installing
ue5 upgrade --force   # Skip the confirmation prompt
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--check` | `-c` | `false` | Only check for updates; do not install |
| `--force` | `-f` | `false` | Skip the confirmation prompt |

### `version`
Print the CLI version and the Unreal Engine version associated with the current project.

```bash
ue5 version
```

---

## Server Mode

The CLI includes a **Server Mode** — a background daemon that manages Unreal Editor instances. It is the foundation for AI-agent-driven development workflows.

### Architecture overview

```
ue5 server start
       │
       ▼
   Daemon (~/.ue5/server.sock)
   ├── Instance Manager  – start/stop/monitor editor processes
   ├── Log Capture       – buffers all stdout/stderr from each editor
   ├── Build Orchestrator– coalesces rebuild requests; runs UBT
   ├── Agent Registry    – tracks connected AI agents
   ├── MCP SSE Server    – port 9515 (Model Context Protocol)
   └── Web Dashboard     – port 9516
```

State is persisted to `~/.ue5/state.json` so it survives daemon restarts. Per-project editor logs are stored under `~/.ue5/logs/<project-hash>/`.

### Quick start

```bash
ue5 server start                               # Start the daemon in the background
ue5 server run --wait --timeout 120s --json    # Launch editor; wait until running
ue5 server status                              # Check daemon and instance health
ue5 server rebuild --label "Added feature" --mode full
ue5 server build-info --json                   # Query build metadata
ue5 server logs --level error --since 5m       # Query captured logs
ue5 server logs --follow                       # Stream logs in real-time
ue5 server kill                                # Stop the editor for the current project
ue5 server kill --all                          # Stop all managed editor instances
ue5 server agents --json                       # List registered AI agents
ue5 server stop                                # Shut down the daemon
```

### Server subcommands

#### `server start`
Start the daemon in the background. If the daemon is already running, reports its current status.

```bash
ue5 server start
ue5 server start --foreground   # Run in foreground (useful for debugging)
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--foreground` | `-f` | `false` | Run the daemon in the foreground (blocking) |

#### `server stop`
Gracefully shut down the daemon and all managed editor instances.

```bash
ue5 server stop
```

#### `server status`
Show the daemon status and all managed editor instances.

```bash
ue5 server status
ue5 server status --json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output status as JSON |

#### `server run`
Start the Unreal Editor for the current project, managed by the daemon. The daemon is auto-started if it is not already running.

```bash
ue5 server run
ue5 server run --wait --timeout 120s
ue5 server run --wait --json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--wait` | `false` | Block until the editor reaches the `running` state |
| `--timeout` | `90s` | Maximum wait duration when `--wait` is set |
| `--poll-interval` | `1s` | How often to check editor state when `--wait` is set |
| `--json` | `false` | Output instance info as JSON |

#### `server kill`
Stop one or all managed editor instances.

```bash
ue5 server kill              # Stop the editor for the current project
ue5 server kill --all        # Stop all managed instances
ue5 server kill --force      # Force-kill (SIGKILL) instead of graceful shutdown
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--force` | `-f` | `false` | Send SIGKILL instead of SIGTERM |
| `--all` | | `false` | Stop all managed instances |

#### `server rebuild`
Trigger a project rebuild through the daemon.

**Modes:**
- `full` — Stop the editor, run Unreal Build Tool (UBT), then restart the editor.
- `hot_reload` — Build in the background while the editor continues running.

When multiple agents request rebuilds concurrently, the daemon **coalesces** them into a single build, collecting all labels.

```bash
ue5 server rebuild --label "Added inventory system" --mode full
ue5 server rebuild --label "Fixed AI navigation" --mode hot_reload
ue5 server rebuild --label "New weapon component" --mode full --agent my-agent-id
```

| Flag | Default | Description |
|------|---------|-------------|
| `--label` | *(required)* | Human-readable description of what changed |
| `--mode` | `full` | Build mode: `full` or `hot_reload` |
| `--agent` | | Agent ID requesting the rebuild |
| `--target` | Inherits global `-t` | Build target override |
| `--state` | Inherits global `-s` | Build configuration override |

#### `server logs`
Query or stream captured logs from a managed editor instance.

```bash
ue5 server logs                            # Show last 100 lines
ue5 server logs -n 50                      # Show last 50 lines
ue5 server logs --level error              # Show only errors
ue5 server logs --category LogCompile      # Filter by UE log category
ue5 server logs --pattern "shader"         # Filter by regex pattern
ue5 server logs --since 5m                 # Only lines from the last 5 minutes
ue5 server logs --follow                   # Stream new lines in real-time (like tail -f)
ue5 server logs --json                     # Output as JSON
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--lines` | `-n` | `100` | Number of recent lines to show |
| `--follow` | `-f` | `false` | Stream new log lines in real-time |
| `--level` | `-l` | | Filter by level: `error`, `warning`, `info` |
| `--category` | `-c` | | Filter by UE log category (e.g. `LogCompile`) |
| `--pattern` | | | Filter by regex pattern |
| `--since` | | | Only show lines after this time (duration like `5m` or RFC3339 timestamp) |
| `--json` | | `false` | Output as JSON |

#### `server build-info`
Show current build metadata and the accumulated feature history tracked across all builds.

```bash
ue5 server build-info
ue5 server build-info --json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output as JSON |

#### `server agents`
List all AI agents currently registered with the daemon.

```bash
ue5 server agents
ue5 server agents --json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--json` | `false` | Output as JSON |

---

## MCP Integration (AI Agents)

The daemon exposes a [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) server over Server-Sent Events on port **9515** (override with `UE5_MCP_PORT`). AI coding agents can connect to it to drive the editor programmatically.

### MCP Tools

| Tool | Description |
|------|-------------|
| `rebuild` | Trigger a project rebuild (`full` or `hot_reload`). Concurrent requests are coalesced. |
| `register_agent` | Register an AI agent to receive notifications about editor state changes and builds. |
| `unregister_agent` | Unregister an AI agent. |
| `get_build_info` | Retrieve current build metadata and accumulated feature history. |

### MCP Resources

| URI | Description |
|-----|-------------|
| `ue5://build/current` | Current build metadata, accumulated features, and status. |
| `ue5://agents` | List of currently registered AI agents. |
| `ue5://instances` | Currently managed editor instances and their states. |

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `UE5_MCP_PORT` | `9515` | Port for the MCP SSE server |
| `UE5_DASHBOARD_PORT` | `9516` | Port for the web dashboard |

---

## Dashboard

The daemon ships a web dashboard that shows real-time build status, editor instances, and connected AI agents. It starts automatically with the daemon on port **9516** (override with the `UE5_DASHBOARD_PORT` environment variable).

Key features:
- **Live updates** via Server-Sent Events (SSE) — no polling required
- **Trigger rebuilds** and **start/stop the editor** directly from the UI

**Development** — run the Vite dev server with hot reload (proxies API requests to the Go backend on `:9516`):

```bash
cd dashboard && npm run dev
```

**Production** — embed the compiled frontend into the Go binary:

```bash
scripts/build-dashboard.sh
```

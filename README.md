# UE5 CLI
## Unreal Engine 5 Command Line Interface

A command line tool for Unreal Engine 5 that handles the boring parts of UE5 development. It started as a build/package automator (think Adam Rehn's [ue4 cli](https://docs.adamrehn.com/ue4cli/overview/introduction-to-ue4cli/) but in Go, no Python required) and has grown into a full editor management daemon.

**What you get:**
- **CLI commands** — `build`, `gen`, `package`, `clean`, `run` that auto-detect your engine version from the `.uproject` and Epic Games Launcher manifests
- **Server mode** — a background daemon that manages editor instances, captures all logs, and coordinates AI-driven rebuilds
- **Web dashboard** — real-time view of build status, editor instances, and connected agents at [http://localhost:9516](http://localhost:9516)
- **MCP integration** — push notifications to AI agents over SSE so Claude Code (and other MCP clients) can drive your build-test-debug loop
- **Claude Code plugin** (`uem`) — slash commands, a specialized agent, and skills for UE5 development

## Installation
Prerequisites: Go 1.24+.

From the repo root:
```console
go install .
```

This installs the `ue5` binary to `$(go env GOPATH)/bin` (or `$(go env GOBIN)` if set). Make sure that directory is on your `PATH`.

### Install from GitHub release (no repo clone)

One-liner for macOS / Linux — resolves the latest tag, downloads the right asset for your OS/arch, and drops the `ue5` binary into `/usr/local/bin`:

```bash
TAG=$(curl -fsSL https://api.github.com/repos/Benbentwo/ue5/releases/latest | grep '"tag_name"' | cut -d'"' -f4) \
  && OS=$(uname -s | tr '[:upper:]' '[:lower:]') \
  && ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') \
  && curl -fsSL "https://github.com/Benbentwo/ue5/releases/download/${TAG}/ue5_${TAG#v}_${OS}_${ARCH}.tar.gz" \
     | sudo tar xz -C /usr/local/bin ue5
```

Verify: `ue5 version`.

Windows users: grab the `.tar.gz` for `windows_amd64` from the [Releases page](https://github.com/Benbentwo/ue5/releases/latest) and extract `ue5.exe` somewhere on your `PATH`.

### Install the Claude Code plugin (`uem`)

The repo ships a Claude Code plugin called **`uem`** (Unreal Editor Manager) that turns the `ue5` daemon into a first-class part of your Claude Code workflow:

- **Slash commands** — `/server`, `/start`, `/stop`, `/rebuild`, `/logs` drive the daemon without leaving Claude
- **`ue5-dev-assistant` agent** — autonomously runs the build → test → debug loop, querying captured logs and triggering rebuilds via the daemon
- **`ue5-development` skill** — activates automatically when Claude is working in a UE5 project, surfacing daemon-aware patterns

The plugin assumes the `ue5` binary is on your `PATH` (install it first via the one-liner above).

**Install via the Claude Code marketplace** (recommended):

```text
/plugin marketplace add Benbentwo/ue5
/plugin install uem@ue5
```

This pulls the marketplace manifest from this repo, registers `uem`, and installs it. Updates are a single `/plugin update uem@ue5` away.

**Install from a local clone** (for plugin development):

```bash
git clone https://github.com/Benbentwo/ue5.git ~/src/ue5
ln -s ~/src/ue5/plugin ~/.claude/plugins/uem
```

Either way, confirm with `/plugin list` — you should see `uem` enabled.

## Usage
```conosle
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

## Server Mode

The CLI includes a Server Mode that runs a background daemon to manage Unreal Editor instances. Features include:

- **Editor lifecycle management**: Start, stop, and monitor editor instances
- **Log capture and querying**: All editor stdout/stderr captured, filterable by level, category, pattern
- **AI-driven rebuilds**: Trigger rebuilds with descriptive labels, choose full or hot reload mode
- **Build metadata**: Track accumulated features across builds, query build history
- **Multi-agent coordination**: Multiple AI agents work concurrently with automatic build coalescing
- **MCP integration**: Push notifications to connected AI agents via SSE (port 9515)

```bash
ue5 server start                              # Start the daemon
ue5 server run --wait --timeout 120s --json  # Launch editor and wait for running state
ue5 server rebuild --label "Added feature" --mode full  # Trigger rebuild
ue5 server build-info --json                  # Query build metadata
ue5 server logs --level error --since 5m      # Query captured logs
ue5 server agents --json                      # List registered AI agents
```

See `ue5 server --help` for all available subcommands.

### Log Locations

All server state lives under `~/.ue5/`. Logs are stored per-project using a hash of the `.uproject` path:

| File | Path | Contents |
|------|------|----------|
| Editor log | `~/.ue5/logs/<hash>/editor.log` | Captured editor stdout/stderr |
| Build log | `~/.ue5/logs/<hash>/build.log` | UBT compiler output (errors, warnings, linker output) |
| Daemon state | `~/.ue5/state.json` | Build history, accumulated features, active agents |

The `<hash>` is the first 12 hex characters of `SHA-256(project_path)`. To read a build log directly:

```bash
cat ~/.ue5/logs/$(echo -n "/path/to/YourProject.uproject" | shasum -a 256 | cut -c1-12)/build.log
```

Or use the CLI, which resolves the hash for you:

```bash
ue5 server logs --level error          # Query captured editor logs
ue5 server build-info --json           # Build status and history
```

### Dashboard

The daemon ships a web dashboard that shows real-time build status, editor instances, and connected AI agents. Once the daemon is running (`ue5 server start`), open:

**→ [http://localhost:9516](http://localhost:9516)**

Override the port with the `UE5_DASHBOARD_PORT` environment variable if `9516` conflicts with something else.

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

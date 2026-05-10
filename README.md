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
ue5 server run                                # Launch editor (auto-starts daemon)
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

---
name: verify
description: Verify ue5 CLI/daemon changes end-to-end in a sandbox that cannot touch the live fleet daemon
---

# Verifying ue5 daemon/CLI changes

The daemon serving the IslandSurvival fleet lives at `~/.ue5/server.sock`
(pid in `~/.ue5/daemon.pid`, MCP SSE :9515, dashboard :9516). Never signal
it or bind its ports during verification.

## Sandbox recipe

`HomeDir()` derives everything from `$HOME`, and both listen ports have env
overrides — so a fake home fully isolates a test daemon:

```bash
E2E=/private/tmp/ue5e2e            # SHORT path — macOS unix sockets cap at
mkdir -p $E2E/bin $E2E/home        # 104 bytes; deep paths fail with
go build -o $E2E/bin/ue5 .         # "bind: invalid argument"
export HOME=$E2E/home UE5_MCP_PORT=19515 UE5_DASHBOARD_PORT=19516

$E2E/bin/ue5 server start          # daemon: $E2E/home/.ue5/server.sock
$E2E/bin/ue5 server status --json  # {"daemon_running":true,...,"version":"dev"}
cat $E2E/home/.ue5/daemon.pid      # pid for kill -9 / restart assertions
```

Source builds report version `dev`, which `ue5 upgrade` always considers
outdated — so the real upgrade flow (GitHub download included) is drivable
in the sandbox: it replaces `$E2E/bin/ue5` with the latest release.

## MCP SSE probe (agent surface)

```bash
curl -sN http://localhost:19515/sse > sse.log &   # endpoint arrives as
EP=$(grep -m1 "^data: " sse.log | sed 's/^data: //')  # data: /message?sessionId=...
post() { curl -s -X POST "http://localhost:19515$EP" -H 'Content-Type: application/json' -d "$1" >/dev/null; sleep 0.4; }
post '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}'
post '{"jsonrpc":"2.0","method":"notifications/initialized"}'
post '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_build_status","arguments":{}}}'
grep "^data: " sse.log            # responses stream back on the SSE channel
```

## Teardown

```bash
$E2E/bin/ue5 server stop && sleep 1
pgrep -fl "$E2E/bin/ue5" || rm -rf $E2E
```

Confirm the live daemon survived your session:
`kill -0 $(cat ~/.ue5/daemon.pid)`.

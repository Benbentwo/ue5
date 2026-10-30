# Build service (`ue5 ci`)

The build service runs inside the ue5 daemon on a **build box**: a machine that
also runs a self-hosted Azure Pipelines agent (or any other CI agent). A pipeline
job hands a build to the box's own daemon with one local HTTP call and finishes
in seconds, so a limited pool of parallel jobs (the free tier allows one) is
never held by a multi-hour cook, and several boxes build at the same time. The daemon
queues the build, syncs the checkout, runs the game repo's recipe, and reports
the result:

- as a **commit status** on Azure Repos (`ue5-ci/<kind>-<platform>`: pending →
  succeeded / failed), linking to the build's page;
- on the **dashboard**, `http://<box>.local:9516/#/ci`, which also shows every
  peer box on the same page.

```
Azure job (seconds)            build box
──────────────────             ─────────────────────────────────────────────
checkout: none
ue5 ci dispatch ... ──POST──▶  queue ─▶ sync repos ─▶ <game>/ci/<kind>.sh|.ps1
job ends green ("dispatched")           │                    │
                                        └── commit status ◀──┴── build.json, log, errors
```

## Turning a box on

The service is **off** unless `~/.ue5/ci/config.json` says otherwise, so a
developer laptop running the daemon for editor management never takes builds.

```json
{
  "enabled": true,
  "box": "build-mac",
  "work_root": "~/ue5-ci/work",
  "disk_floor_gb": 50,
  "peers": ["http://build-win.local:9516"]
}
```

| Field | Default | Meaning |
|---|---|---|
| `enabled` | `false` | Take builds. |
| `box` | hostname | Name on the dashboard and in commit statuses. |
| `work_root` | `~/ue5-ci/work` | One checkout per repo: `<root>/<Game>`, `<root>/<plugins_dir>/<Name>`. Point it at an existing checkout root to reuse what is already downloaded. |
| `plugins_dir` | `Shared/Plugins` | Where pinned plugin repos go, relative to `work_root`. Matches a `.uproject` with `"AdditionalPluginDirectories": ["../Shared/Plugins"]`. |
| `artifact_root` | `<work_root>/_artifacts` | Each job's package goes to `<artifact_root>/<Game>/<job id>/`. |
| `keep_artifacts` | `3` | Packaged builds kept per game. |
| `disk_floor_gb` | `50` | Below this the queue **holds** (it does not fail every job). |
| `reclaim_command` | none | Run once an hour while below the floor, e.g. `["/bin/bash", "/Users/me/reclaim.sh"]`. |
| `public_url` | `http://<LocalHostName>.local:9516` | Used in commit-status links. Set it (e.g. to the box's IP) if `.local` names do not resolve on your network. |
| `peers` | none | Other boxes' dashboards, shown together. |
| `timeout_minutes` | compile 180, package 480, steam 120 | Per kind. |

Restart the daemon after editing (`ue5 server stop`; it auto-starts on the next
`ue5` command, or at login via autostart).

### Secrets

Secrets never travel with a dispatch. The service looks each one up in order:
environment `UE5_CI_<NAME>`, the macOS Keychain (service `ue5-ci`), then
`~/.ue5/ci/secrets/<name>` (a file only its owner can read; the Windows
fallback until Credential Manager support lands).

| Name | Used for |
|---|---|
| `azure-pat` | Git fetches and commit statuses. Scopes: **Code (Read & write)** — status posting needs write. |

```bash
security add-generic-password -s ue5-ci -a azure-pat -w '<PAT>'
```

Check a box's credentials with `ue5 ci doctor --repo <azure repo url>`: it says
which copy of the PAT is in use (never its value), whether the organization
accepts it, and whether git can read with it. Azure answers a PAT it rejects with
HTTP 203 (a sign-in page), so the dashboard also shows the last reporting error.
Replace a stored PAT with `security add-generic-password -U -s ue5-ci -a azure-pat -w '<PAT>'`
and restart the daemon. Git runs non-interactively, so a bad credential fails the
job with a clear message instead of trying to prompt.

Without a PAT the service still builds (using the user's own git credentials)
but posts no commit statuses; the dashboard says so.

### Dispatch token

`~/.ue5/ci/token` is created (mode 0600) on first start. Reads of the API are
open to the LAN; **dispatch, cancel and retry need the token**. The Azure agent
runs as the same user as the daemon, so it can read the token and another device
on the network cannot. The dashboard asks for it once (paste it) before Cancel /
Retry.

## Windows build boxes

- **Secrets:** store the PAT as a generic credential for the user the daemon runs as:
  `cmdkey /generic:ue5-ci:azure-pat /user:ue5-ci /pass:<PAT>`. The `secrets/` file
  and the `UE5_CI_AZURE_PAT` variable also work.
- **Dispatch token across accounts:** a CI agent service usually runs as
  `NetworkService`, not as the user whose daemon builds. Put the token where both can
  read it: `"token_file": "C:\\ProgramData\\ue5-ci\\token"` in the daemon's config,
  give the agent's account read access (`icacls C:\ProgramData\ue5-ci\token /grant
  "NETWORK SERVICE:R"`), and pass the same path to `ue5 ci dispatch --token-file`
  (or `UE5_CI_TOKEN_FILE`).
- **Install where the agent can run it:** a CI agent service running as another
  account cannot run a binary inside your user profile. Install `ue5.exe` in a
  shared folder (for example `C:\ProgramData\ue5`) on the system `PATH`; `ue5
  upgrade` warns when CI is enabled and the binary sits in your profile.
- **Start at logon:** `ue5 server autostart install` adds a per-user Run entry
  (`HKCU\...\CurrentVersion\Run`, no administrator rights needed) and starts the
  daemon now. It runs as you, unelevated, in your desktop session, so builds get a
  desktop session rather than a service's Session 0 (where some editor tooling
  cannot open windows). The daemon runs without a window and outlives the
  terminal that started it.
- **LFS files:** filled with `CopyFileW`. On a ReFS volume, such as a Windows 11 Dev
  Drive, Windows 11 24H2 and later turn that copy into a block clone at no extra disk
  cost; on NTFS it is a full copy, so leave room for the LFS tree twice.
- **Long paths:** git runs with `core.longpaths=true`. Enable Win32 long paths
  (`LongPathsEnabled`) for the build tools as well.
- **Dashboard from the LAN:** allow the port through the firewall, e.g.
  `New-NetFirewallRule -DisplayName "ue5 dashboard" -Direction Inbound -Protocol TCP
  -LocalPort 9516 -Action Allow -Profile Private`.

On macOS, `ue5 server autostart install` registers a LaunchAgent instead (restarted
if it crashes).

## Queue rules

- **One heavy build per box.** One UE compile or cook uses every core, and every
  game on the box shares one plugins checkout.
- **Newest commit wins.** One queued job per game + kind + platform; a newer
  dispatch marks the older one `superseded`. A running build always finishes.
- **Compile before package before steam**, oldest first within a kind, so a push
  never waits behind a nightly cook.
- **Disk floor holds the queue**, with the reason shown on the waiting job.
- **A daemon restart mid-build** marks that job `interrupted` and queues a retry.

## Sharing the workspace with other tools

While a box moves over, CI agent jobs may still build the old way in the same
`work_root`. Two writers syncing one checkout corrupt it, so the service takes
`<work_root>/.workspace-lock/` for each job and **waits** (the job shows
"waiting for the workspace: in use by …") while anything else holds it. A
holder is a process id; a lock whose process has exited is stale and taken over.
To take part from a shell step, take the lock before touching the checkouts and
name a process that lives as long as your work (for an Azure job, the
`Agent.Worker` process), so the lock frees itself when the job ends:

```bash
lock="$WORK_ROOT/.workspace-lock"
until mkdir "$lock" 2>/dev/null; do
  pid=$(sed -n 's/^pid=//p' "$lock/owner" 2>/dev/null)
  if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then sleep 30; else rm -rf "$lock"; fi
done
printf 'pid=%s\nwho=%s\n' "$HOLDER_PID" "my job" > "$lock/owner"
```

## Recipes (in each game repo)

The service knows nothing game-specific. It runs `ci/<kind>.sh` (macOS/Linux,
under bash) or `ci/<kind>.ps1` (Windows) from the game checkout at the job's
commit, so build steps change in the same commit as the game and an old commit
rebuilds with its own steps. A missing recipe fails the job by name.

Working directory: the game checkout. Exit 0 = succeeded. Environment:

| Variable | Value |
|---|---|
| `UE5_CI` | `1` |
| `UE5_CI_JOB_ID`, `UE5_CI_BOX` | job id, box name |
| `UE5_CI_GAME`, `UE5_CI_KIND`, `UE5_CI_PLATFORM` | from the dispatch |
| `UE5_CI_SHA`, `UE5_CI_BRANCH`, `UE5_CI_SOURCE` | commit, branch, who asked |
| `UE5_CI_ENGINE_ASSOCIATION` | the `.uproject`'s `EngineAssociation`, when found |
| `GAME_ROOT` | the game checkout (physical path) |
| `SHARED_ROOT` | the parent of the plugins dir, e.g. `<work_root>/Shared` |
| `ARTIFACT_DIR` | put packaged output here; `build.json` is written beside it |

Failed builds get their key error lines (compiler/UBT errors, `ERROR:`, test
`FAIL`, Python tracebacks, `No space left on device`, ...) pulled out for the
dashboard and the commit status.

## Dispatching

```bash
ue5 ci dispatch --game MyGame --kind compile --platform Mac \
  --repo "$(Build.Repository.Uri)" --sha "$(Build.SourceVersion)" \
  --branch "$(Build.SourceBranch)" \
  --plugin SharedPlugins=https://dev.azure.com/MyOrg/MyProject/_git/SharedPlugins@$PLUGINS_SHA \
  --source "azure:$(Build.BuildId)"
```

It fails (non-zero) at once if the daemon is not answering, the box is off, or
the request is malformed, so the dispatching pipeline run goes red.

```bash
ue5 ci jobs [--game X] [--limit N]
ue5 ci cancel <job-id>
ue5 ci retry <job-id>
```

### API

| Method | Path | Auth |
|---|---|---|
| GET | `/api/ci/status` | open |
| GET | `/api/ci/jobs?limit=&game=` | open |
| GET | `/api/ci/jobs/{id}` | open |
| GET | `/api/ci/jobs/{id}/log?offset=N` or `?tail=N` | open (`X-Log-Size` header carries the length) |
| POST | `/api/ci/jobs` | token |
| POST | `/api/ci/jobs/{id}/cancel` | token |
| POST | `/api/ci/jobs/{id}/retry` | token |
| GET | `/api/ci/jobs/{id}/artifacts` | token (lists the job's artifact files) |
| GET | `/api/ci/jobs/{id}/artifacts.tar` | token (streams the artifact directory as a tar) |

### Using another box's artifacts

Packages are not public, so artifact downloads need the serving box's dispatch
token. A recipe on one box can pull what another box built for the same commit,
for example a store upload that ships a package built on a different OS:

```
ue5 ci fetch --from http://otherbox.local:9516 --from-token-file other-box.token \
  --game MyGame --sha <commit> --kind package --platform Mac --dest <dir>
```

It picks the newest succeeded job that matches and extracts its artifacts into
`--dest`. Symlinks (a Mac app bundle has them) arrive as the files they point at,
so a box that cannot create symlinks can still receive the package.

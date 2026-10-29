package server

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
)

// UBT decides at startup whether to build in hot-reload mode by scanning
// Engine/Intermediate/EditorRuns for live processes whose executable is the
// engine's UnrealEditor binary (HotReload.cs ShouldDoHotReloadFromIDE). It
// does NOT check which project those editors have open. If any such editor is
// alive when UBT starts, the build emits libUnrealEditor-<Module>-000N patch
// binaries instead of relinking the bases, and a subsequently restarted
// editor silently runs stale code. builder.go neutralizes that decision by
// passing -NoHotReloadFromIDE on every full build, so the gate below only has
// to guarantee the OWN project's editors are dead before UBT starts (their
// in-memory modules would still be relinked out from under them); editors
// from other projects on the same engine are tolerated.
const (
	editorExitGrace    = 20 * time.Second // SIGTERM -> SIGKILL escalation point
	editorExitTimeout  = 45 * time.Second // total budget before refusing to build
	editorPollInterval = 500 * time.Millisecond
)

// scanEditorProcesses is the process-scan seam; swappable in tests.
var scanEditorProcesses = findEditorProcesses

// editorProc is one live UnrealEditor process found on the system.
type editorProc struct {
	PID  int
	Args string
}

// findEditorProcesses lists live processes whose executable is editorBinary,
// split into those with projectPath on their command line and all others.
// Unlike InstanceManager, this sees processes the daemon never spawned
// (orphans from a previous daemon, manual launches).
func findEditorProcesses(editorBinary, projectPath string) (project []editorProc, others []editorProc, err error) {
	if runtime.GOOS == "windows" {
		// Process sweep is not implemented on Windows; the -NoHotReloadFromIDE
		// UBT flag still protects full builds there.
		return nil, nil, nil
	}

	out, err := exec.Command("ps", "-axww", "-o", "pid=,args=").Output()
	if err != nil {
		return nil, nil, fmt.Errorf("ps failed: %w", err)
	}
	project, others = parseEditorProcesses(string(out), editorBinary, projectPath)
	return project, others, nil
}

// parseEditorProcesses splits `ps -axww -o pid=,args=` output into editor
// processes for the given project vs. other editors from the same binary.
func parseEditorProcesses(psOutput, editorBinary, projectPath string) (project []editorProc, others []editorProc) {
	for _, line := range strings.Split(psOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		args := strings.TrimSpace(fields[1])
		// The command must BE the editor binary, not merely mention it
		// (e.g. a tail/lsof on its path). Prefix match tolerates arguments
		// after the path; the engine path itself may contain spaces.
		if !strings.HasPrefix(args, editorBinary) {
			continue
		}
		proc := editorProc{PID: pid, Args: args}
		if strings.Contains(args, projectPath) {
			project = append(project, proc)
		} else {
			others = append(others, proc)
		}
	}
	return project, others
}

// ensureNoEditorProcesses blocks until no process running editorBinary has
// projectPath open. Own-project editors are terminated: SIGTERM first
// (graceful editor shutdown, no restore prompt on relaunch), SIGKILL after
// editorExitGrace. Editors from the same engine with a DIFFERENT project
// open are tolerated with a warning: builder.go always passes
// -NoHotReloadFromIDE to UBT on full builds, which forces a base-binary
// link regardless of UBT's engine-wide EditorRuns scan, so their presence
// can no longer flip the build into emitting hot-reload patch binaries.
func ensureNoEditorProcesses(editorBinary, projectPath string) error {
	deadline := time.Now().Add(editorExitTimeout)
	killAfter := time.Now().Add(editorExitGrace)
	signalled := map[int]bool{}
	sentKill := false

	for {
		project, others, err := scanEditorProcesses(editorBinary, projectPath)
		if err != nil {
			return fmt.Errorf("editor process scan failed: %w", err)
		}

		if len(project) == 0 && len(others) == 0 {
			if sentKill {
				// A SIGKILLed editor leaves PackageRestoreData.json behind;
				// on relaunch it raises a modal "Restore Packages" dialog that
				// parks the editor before any automation can reach it.
				quarantinePackageRestoreData(filepath.Dir(projectPath))
			}
			return nil
		}

		if len(project) == 0 && len(others) > 0 {
			log.Warn("Other-project UnrealEditor process(es) running on this engine; -NoHotReloadFromIDE guarantees a base-binary link, proceeding", "pids", procPIDs(others))
			if sentKill {
				quarantinePackageRestoreData(filepath.Dir(projectPath))
			}
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("UnrealEditor process(es) %v still alive after %s", procPIDs(project), editorExitTimeout)
		}

		for _, p := range project {
			proc, findErr := os.FindProcess(p.PID)
			if findErr != nil {
				continue
			}
			switch {
			case time.Now().After(killAfter):
				log.Warn("Editor did not exit after SIGTERM, sending SIGKILL", "pid", p.PID)
				_ = proc.Kill()
				sentKill = true
			case !signalled[p.PID]:
				log.Warn("Terminating untracked editor before full build", "pid", p.PID)
				_ = proc.Signal(syscall.SIGTERM)
				signalled[p.PID] = true
			}
		}

		time.Sleep(editorPollInterval)
	}
}

func procPIDs(procs []editorProc) []int {
	pids := make([]int, 0, len(procs))
	for _, p := range procs {
		pids = append(pids, p.PID)
	}
	return pids
}

// quarantinePackageRestoreData renames Saved/Autosaves/PackageRestoreData.json
// aside (never deletes) so a relaunched editor boots without the modal
// restore prompt. Real assets are saved per-asset by the MCP save paths; the
// restore data only covers crash-recovery temps.
func quarantinePackageRestoreData(projectDir string) {
	p := filepath.Join(projectDir, "Saved", "Autosaves", "PackageRestoreData.json")
	if _, err := os.Stat(p); err != nil {
		return
	}
	bak := p + ".quarantined-" + time.Now().Format("20060102-150405")
	if err := os.Rename(p, bak); err != nil {
		log.Warn("Failed to quarantine package restore data", "path", p, "error", err)
		return
	}
	log.Warn("Quarantined package restore data left by SIGKILL", "moved_to", bak)
}

// hotReloadPatchRe matches UBT hot-reload patch binaries: the base module
// binary name plus a -NNNN numeric suffix (libUnrealEditor-Foo-0001.dylib,
// UnrealEditor-Foo-1234.dll, and their .patch_N variants are all covered by
// the numeric-suffix rule).
var hotReloadPatchRe = regexp.MustCompile(`^(?:lib)?UnrealEditor-.+-\d{4}\.(?:dylib|so|dll|pdb)$`)

// cleanHotReloadArtifacts deletes stale hot-reload patch binaries under the
// project's Binaries/ and Plugins/ trees so that .modules manifests written
// by the upcoming build can only reference freshly linked base binaries.
// Returns the paths removed.
func cleanHotReloadArtifacts(projectDir string) []string {
	skipDirs := map[string]bool{
		"Content":          true,
		"Intermediate":     true,
		"Saved":            true,
		"Source":           true,
		"Config":           true,
		"Resources":        true,
		"Shaders":          true,
		"DerivedDataCache": true,
		".git":             true,
	}

	var removed []string
	for _, root := range []string{filepath.Join(projectDir, "Binaries"), filepath.Join(projectDir, "Plugins")} {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skipDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if hotReloadPatchRe.MatchString(d.Name()) {
				if rmErr := os.Remove(path); rmErr == nil {
					removed = append(removed, path)
				} else {
					log.Warn("Failed to remove hot-reload patch binary", "path", path, "error", rmErr)
				}
			}
			return nil
		})
	}
	return removed
}

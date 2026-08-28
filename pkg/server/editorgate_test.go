package server

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	testEditorBinary = "/Users/Shared/Epic Games/UE_5.8/Engine/Binaries/Mac/UnrealEditor.app/Contents/MacOS/UnrealEditor"
	testProjectPath  = "/Users/dev/IslandSurvival/IslandSurvival.uproject"
)

func TestParseEditorProcesses(t *testing.T) {
	psOutput := `  100 /sbin/launchd
35450 ` + testEditorBinary + ` ` + testProjectPath + `
35460 ` + testEditorBinary + ` /Users/dev/OtherGame/OtherGame.uproject
35470 ` + testEditorBinary + `
90404 /Users/dev/UnrealEngine/Engine/Binaries/Mac/UnrealEditorServices.app/Contents/MacOS/UnrealEditorServices
98025 /Applications/Epic Games Launcher.app/Contents/UE/Engine/Binaries/Mac/EpicWebHelper --type=renderer
50000 tail -f ` + testEditorBinary + `.log
`

	project, others := parseEditorProcesses(psOutput, testEditorBinary, testProjectPath)

	if len(project) != 1 || project[0].PID != 35450 {
		t.Errorf("expected exactly project editor PID 35450, got %+v", project)
	}
	// 35460 (other project) and 35470 (no project on cmdline) are both
	// "other" editors; UnrealEditorServices, EpicWebHelper, and the tail
	// process must not match at all.
	if len(others) != 2 {
		t.Fatalf("expected 2 other editors, got %+v", others)
	}
	if others[0].PID != 35460 || others[1].PID != 35470 {
		t.Errorf("unexpected other-editor PIDs: %+v", others)
	}
}

func TestParseEditorProcessesEmpty(t *testing.T) {
	project, others := parseEditorProcesses("  100 /sbin/launchd\n", testEditorBinary, testProjectPath)
	if len(project) != 0 || len(others) != 0 {
		t.Errorf("expected no matches, got project=%+v others=%+v", project, others)
	}
}

func TestHotReloadPatchRe(t *testing.T) {
	matches := []string{
		"libUnrealEditor-SadTire_MCP-0001.dylib",
		"libUnrealEditor-IslandSurvivalCore-0012.dylib",
		"UnrealEditor-MyModule-4821.dll",
		"UnrealEditor-MyModule-4821.pdb",
	}
	nonMatches := []string{
		"libUnrealEditor-SadTire_MCP.dylib",
		"UnrealEditor-MyModule.dll",
		"UnrealEditor.modules",
		"libUnrealEditor-Foo-001.dylib", // 3 digits: not a patch suffix
		"libSomeOtherLib-0001.dylib",    // not an UnrealEditor module
		"libUnrealEditor-Foo-0001.dylib.bak",
	}
	for _, name := range matches {
		if !hotReloadPatchRe.MatchString(name) {
			t.Errorf("expected %q to match", name)
		}
	}
	for _, name := range nonMatches {
		if hotReloadPatchRe.MatchString(name) {
			t.Errorf("expected %q NOT to match", name)
		}
	}
}

func TestCleanHotReloadArtifacts(t *testing.T) {
	projectDir := t.TempDir()

	write := func(rel string) string {
		p := filepath.Join(projectDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	patch1 := write("Binaries/Mac/libUnrealEditor-Game-0001.dylib")
	patch2 := write("Plugins/SadTirePlugins/SadTire_MCP/Binaries/Mac/libUnrealEditor-SadTire_MCP-0003.dylib")
	base := write("Plugins/SadTirePlugins/SadTire_MCP/Binaries/Mac/libUnrealEditor-SadTire_MCP.dylib")
	// A pruned directory must never be touched, even with a matching name.
	prunedDecoy := write("Plugins/SadTirePlugins/SadTire_MCP/Content/libUnrealEditor-Decoy-0001.dylib")

	removed := cleanHotReloadArtifacts(projectDir)

	if len(removed) != 2 {
		t.Fatalf("expected 2 removals, got %v", removed)
	}
	for _, gone := range []string{patch1, patch2} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed", gone)
		}
	}
	for _, kept := range []string{base, prunedDecoy} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("expected %s to survive: %v", kept, err)
		}
	}
}

func TestEnsureNoEditorProcessesCleanSystem(t *testing.T) {
	// No editor binary at this fake path can be running; the gate must pass
	// immediately without signalling anything.
	if err := ensureNoEditorProcesses("/nonexistent/FakeEngine/UnrealEditor", testProjectPath); err != nil {
		t.Errorf("expected clean pass, got %v", err)
	}
}

func TestEnsureNoEditorProcessesToleratesOtherProjects(t *testing.T) {
	// Multi-instance: other projects' editors on the same engine are tolerated
	// (builder.go passes -NoHotReloadFromIDE on full builds), not a hard error.
	orig := scanEditorProcesses
	scanEditorProcesses = func(editorBinary, projectPath string) ([]editorProc, []editorProc, error) {
		return nil, []editorProc{{PID: 12345, Args: editorBinary + " /Users/dev/OtherGame/OtherGame.uproject"}}, nil
	}
	defer func() { scanEditorProcesses = orig }()

	if err := ensureNoEditorProcesses(testEditorBinary, testProjectPath); err != nil {
		t.Errorf("expected other-project editors to be tolerated, got %v", err)
	}
}

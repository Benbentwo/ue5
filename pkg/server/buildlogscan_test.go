package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real thing, verbatim from ~/.ue5/logs/f19db471355f/build.log on
// 2026-09-01, in the format LogCapture actually writes to disk
// ("<rfc3339> <stream> | <raw>"). Using the captured format rather than bare
// UBT text is the point: a test fed bare text passes against a scanner that
// cannot read a real log.
const capturedSegfaultLog = `2026-09-01T13:09:07.100-07:00 stdout | Building GrimHoldEditor...
2026-09-01T13:09:08.200-07:00 stdout | Module 'Http' (referenced via GrimHoldEditor -> BpGeneratorUltimate.Build.cs) has incorrect text case. Did you mean 'HTTP'?
2026-09-01T13:09:09.300-07:00 stderr | ERROR: Segmentation fault
2026-09-01T13:09:09.310-07:00 stderr |  CALLSTACK:
2026-09-01T13:09:09.320-07:00 stderr |    /Users/Shared/Epic Games/UE_5.8/Engine/Binaries/ThirdParty/DotNet/10.0/mac-arm64/shared/Microsoft.NETCore.App/10.0.7/libcoreclr.dylib: 0x1ef6d0
`

const capturedCleanLog = `2026-09-01T13:11:00.100-07:00 stdout | Building GrimHoldEditor...
2026-09-01T13:11:40.900-07:00 stdout | [1/3] Compile SadTire_GroupCamera.cpp
2026-09-01T13:11:59.000-07:00 stdout | Total execution time: 52.31 seconds
`

// TestRebuildScanFailsOnSegfaultedUBT is the unit half: does this text mean
// failure?
func TestRebuildScanFailsOnSegfaultedUBT(t *testing.T) {
	marker, line := scanFatalMarkers(strings.NewReader(capturedSegfaultLog))
	if marker == "" {
		t.Fatalf("a log carrying 'ERROR: Segmentation fault' must be read as a failure, got no marker")
	}
	if !strings.Contains(line, "Segmentation fault") {
		t.Errorf("the reported line must quote the crash, got %q", line)
	}
}

// TestRebuildScanFailsNoFalsePositiveOnCleanLog is the other half, and the more
// important one: a scanner that flagged everything would "fix" the bug by
// failing every build, and every test above would still pass.
func TestRebuildScanFailsNoFalsePositiveOnCleanLog(t *testing.T) {
	if marker, line := scanFatalMarkers(strings.NewReader(capturedCleanLog)); marker != "" {
		t.Fatalf("a clean build log must produce no marker, got %q from %q", marker, line)
	}

	// A compiler diagnostic that QUOTES the text "ERROR: " mid-line is not a
	// build failure. The match is anchored for exactly this case.
	quoted := `2026-09-01T13:11:40.900-07:00 stdout | note: expanded from macro 'CHECK_ERROR: do {} while(0)'
`
	if marker, line := scanFatalMarkers(strings.NewReader(quoted)); marker != "" {
		t.Fatalf("a mid-line 'ERROR: ' must not count, got %q from %q", marker, line)
	}
}

// TestRebuildFailsWritesFailedStatusAndError is the integration half: a build
// that dies must reach ~/.ue5/state.json as status=failed WITH a non-empty
// error. Status alone is not enough -- an empty error string sends the next
// reader back to a log they do not know to look at.
func TestRebuildFailsWritesFailedStatusAndError(t *testing.T) {
	dir := t.TempDir()

	state := NewStateStore()
	state.path = filepath.Join(dir, "state.json")
	agents := NewAgentRegistry()
	manager := NewInstanceManager()
	b := NewBuildOrchestrator(manager, state, agents)

	// Stand in for a UBT that segfaulted while Build.sh exited zero: runBuild's
	// log scan is what turns that into an error, and this is the error it makes.
	marker, line := scanFatalMarkers(strings.NewReader(capturedSegfaultLog))
	if marker == "" {
		t.Fatalf("fixture problem: the captured log must carry a marker")
	}
	b.buildRunner = func(record *BuildRecord) error {
		return fatalMarkerError(marker, line)
	}

	record := b.createRecord([]RebuildRequest{{
		ProjectPath: filepath.Join(dir, "MyGame.uproject"),
		// FULL, because that is the path the buildRunner hook covers and the path
		// `ue5 server rebuild --mode full` takes -- the mode the incident happened on.
		Mode:        BuildModeFull,
		Label:       "segfault repro",
		AgentID:     "test",
	}})

	b.executeBuild(context.Background(), record)

	// Read it back the way a consumer does: state.json's current_build, which is
	// the field the dispatcher's own build poll greps.
	current := state.GetCurrentBuild()
	if current == nil {
		t.Fatalf("no current build in the state store after executeBuild")
	}
	if current.ID != record.ID {
		t.Fatalf("current build is %s, expected %s", current.ID, record.ID)
	}
	if current.Status != BuildStatusFailed {
		t.Errorf("a build whose UBT died must be status %q, got %q",
			BuildStatusFailed, current.Status)
	}
	if strings.TrimSpace(current.Error) == "" {
		t.Errorf("a failed build must carry a non-empty error; a bare status tells the next reader nothing")
	}
	if !strings.Contains(current.Error, "Segmentation fault") {
		t.Errorf("the recorded error must name what actually happened, got %q", current.Error)
	}
}

// TestRebuildFailsScanReadsLogTail proves the on-disk path, not just the reader:
// the scan must find a marker in a real file, including one large enough to hit
// the tail-seek branch.
func TestRebuildFailsScanReadsLogTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build.log")

	var sb strings.Builder
	filler := "2026-09-01T13:09:07.100-07:00 stdout | [1/9999] Compile Something.cpp\n"
	for sb.Len() < maxScanBytes+(64*1024) {
		sb.WriteString(filler)
	}
	sb.WriteString(capturedSegfaultLog)

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("writing fixture log: %v", err)
	}

	marker, line := scanBuildLogForFatal(path)
	if marker == "" {
		t.Fatalf("the crash is in the log TAIL and must be found there; got no marker")
	}
	if !strings.Contains(line, "Segmentation fault") {
		t.Errorf("got %q", line)
	}

	// An unreadable log must NOT invent a failure.
	if marker, _ := scanBuildLogForFatal(filepath.Join(t.TempDir(), "absent.log")); marker != "" {
		t.Errorf("a missing log is not evidence of a failed build, got marker %q", marker)
	}
}

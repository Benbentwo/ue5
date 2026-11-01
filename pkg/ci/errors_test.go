package ci

import (
	"os"
	"path/filepath"
	"testing"
)

// The build tool probes for a git repository and ignores the answer; the note
// must show the line that really failed the job.
func TestExtractErrorsSkipsTheBuildToolGitProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.log")
	log := "fatal: not a git repository (or any of the parent directories): .git\n" +
		"RunUAT ERROR: AutomationTool was unable to run successfully. Exited with code: 6\n"
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ExtractErrors(path, 1)
	if len(got) != 1 || got[0] != "RunUAT ERROR: AutomationTool was unable to run successfully. Exited with code: 6" {
		t.Fatalf("note = %q", got)
	}
}

package ci

import (
	"os"
	"strings"
	"testing"
)

func TestWithToolPathAddsMissingToolDirs(t *testing.T) {
	sep := string(os.PathListSeparator)
	exists := func(d string) bool { return d == "/opt/homebrew/bin" || d == "/usr/bin" }
	got := withToolPath("/usr/bin"+sep+"/bin", exists)
	if !strings.HasPrefix(got, "/usr/bin"+sep+"/bin") {
		t.Fatalf("existing entries must stay first: %q", got)
	}
	if !strings.Contains(got, "/opt/homebrew/bin") {
		t.Fatalf("missing tool dir not added: %q", got)
	}
	if strings.Count(got, "/usr/bin") != 1 {
		t.Fatalf("an entry already present must not repeat: %q", got)
	}
	if strings.Contains(got, "/usr/local/bin") {
		t.Fatalf("a dir that does not exist must not be added: %q", got)
	}
	if got := withToolPath("", exists); got != "/opt/homebrew/bin"+sep+"/usr/bin" {
		t.Fatalf("empty PATH: %q", got)
	}
}

//go:build !windows

package ci

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestService builds a service whose "sync" writes the given recipe into
// the game checkout, so the runner can be driven end to end without git.
func newTestService(t *testing.T, recipe string) *Service {
	t.Helper()
	home := t.TempDir()
	root := t.TempDir()
	cfg := Config{Enabled: true, Box: "box", WorkRoot: root}
	cfg.fill()
	s, err := NewService(home, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.Reporter = &Reporter{}
	s.freeFn = func(string) (int, error) { return 500, nil }
	s.syncFn = func(ctx context.Context, j *Job, log io.Writer) error {
		dir := filepath.Join(root, j.Game, "ci")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if recipe == "" {
			return nil
		}
		return os.WriteFile(filepath.Join(dir, string(j.Kind)+".sh"), []byte(recipe), 0o755)
	}
	return s
}

func runOne(t *testing.T, s *Service, r Request) *Job {
	t.Helper()
	j, err := s.Dispatch(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	next, ok := s.Queue.Next("")
	if !ok || next.ID != j.ID {
		t.Fatal("expected the dispatched job to start")
	}
	s.run(context.Background(), next)
	done, _ := s.Queue.Get(j.ID)
	return done
}

func TestRunSucceedsAndPassesTheRecipeContract(t *testing.T) {
	s := newTestService(t, `#!/bin/bash
set -eu
echo "game=$UE5_CI_GAME kind=$UE5_CI_KIND platform=$UE5_CI_PLATFORM sha=$UE5_CI_SHA"
test "$(pwd -P)" = "$(cd "$GAME_ROOT" && pwd -P)"
echo packaged > "$ARTIFACT_DIR/Game.zip"
`)
	j := runOne(t, s, req("MyGame", KindPackage, 'a'))
	if j.State != StateSucceeded || j.ExitCode == nil || *j.ExitCode != 0 {
		log, _ := os.ReadFile(j.LogPath)
		t.Fatalf("state %s exit %v msg %q\n%s", j.State, j.ExitCode, j.Message, log)
	}
	log, _ := os.ReadFile(j.LogPath)
	if !strings.Contains(string(log), "game=MyGame kind=package platform=Win64 sha="+sha('a')) {
		t.Fatalf("recipe env missing from log:\n%s", log)
	}
	var meta Job
	b, err := os.ReadFile(filepath.Join(j.ArtifactDir, "build.json"))
	if err != nil || json.Unmarshal(b, &meta) != nil || meta.ID != j.ID || meta.State != StateSucceeded {
		t.Fatalf("build.json beside the package: %v %s", err, b)
	}
}

func TestRunFailureExtractsErrorLines(t *testing.T) {
	s := newTestService(t, `#!/bin/bash
echo "Building 412 actions"
echo "LogInit: Display: error handling enabled"
echo "C:\\s\\Foo.cpp(172,46): error C4458: declaration of 'Owner' hides class member"
echo "Result: Failed (OtherCompilationError)"
exit 6
`)
	j := runOne(t, s, req("OtherGame", KindCompile, 'b'))
	if j.State != StateFailed || j.ExitCode == nil || *j.ExitCode != 6 {
		t.Fatalf("state %s exit %v", j.State, j.ExitCode)
	}
	if len(j.Errors) != 2 || !strings.Contains(j.Errors[0], "C4458") || !strings.Contains(j.Errors[1], "Result: Failed") {
		t.Fatalf("errors %q", j.Errors)
	}
	if _, err := os.Stat(j.ArtifactDir); !os.IsNotExist(err) {
		t.Fatal("an empty artifact dir must not be left behind")
	}
}

func TestMissingRecipeFailsByName(t *testing.T) {
	s := newTestService(t, "")
	j := runOne(t, s, req("FourthGame", KindSteam, 'c'))
	if j.State != StateFailed || !strings.Contains(j.Message, "no recipe ci/steam.sh") {
		t.Fatalf("state %s msg %q", j.State, j.Message)
	}
}

func TestCancelStopsARunningRecipe(t *testing.T) {
	s := newTestService(t, "#!/bin/bash\nsleep 30 &\nwait\n")
	j, _ := s.Dispatch(context.Background(), req("MyGame", KindCompile, 'a'))
	next, _ := s.Queue.Next("")
	done := make(chan struct{})
	go func() { s.run(context.Background(), next); close(done) }()
	for i := 0; s.Running() == nil; i++ {
		if i > 500 {
			t.Fatal("the job never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := s.Cancel(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not stop the recipe and its children")
	}
	got, _ := s.Queue.Get(j.ID)
	if got.State != StateCanceled {
		t.Fatalf("state %s", got.State)
	}
}

func TestDiskHold(t *testing.T) {
	s := newTestService(t, "#!/bin/bash\n")
	s.freeFn = func(string) (int, error) { return 10, nil }
	if why := s.diskHold(); !strings.Contains(why, "10 GB free") {
		t.Fatalf("hold %q", why)
	}
	s.freeFn = func(string) (int, error) { return 60, nil }
	if why := s.diskHold(); why != "" {
		t.Fatalf("no hold expected, got %q", why)
	}
}

func TestPruneArtifactsKeepsNewest(t *testing.T) {
	s := newTestService(t, "")
	s.Config.KeepArtifacts = 2
	for _, id := range []string{"20261001-000000-a", "20261002-000000-b", "20261003-000000-c"} {
		_ = os.MkdirAll(filepath.Join(s.Config.ArtifactRoot, "MyGame", id), 0o755)
	}
	s.pruneArtifacts("MyGame")
	entries, _ := os.ReadDir(filepath.Join(s.Config.ArtifactRoot, "MyGame"))
	if len(entries) != 2 || entries[0].Name() != "20261002-000000-b" {
		t.Fatalf("kept %v", entries)
	}
}

func TestHTTPDispatchNeedsTokenAndReadsAreOpen(t *testing.T) {
	s := newTestService(t, "")
	h := &Handler{Service: s, Token: "tok"}
	mux := http.NewServeMux()
	h.Register(mux, func(f http.HandlerFunc) http.HandlerFunc { return f })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body, _ := json.Marshal(req("MyGame", KindCompile, 'a'))
	resp, _ := http.Post(srv.URL+"/api/ci/jobs", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", resp.StatusCode)
	}
	r, _ := http.NewRequest("POST", srv.URL+"/api/ci/jobs", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer tok")
	resp, _ = http.DefaultClient.Do(r)
	var v JobView
	_ = json.NewDecoder(resp.Body).Decode(&v)
	if resp.StatusCode != http.StatusAccepted || v.State != StateQueued || v.Position != 1 {
		t.Fatalf("dispatch: %d %+v", resp.StatusCode, v)
	}
	resp, _ = http.Get(srv.URL + "/api/ci/jobs/" + v.ID)
	if resp.StatusCode != 200 {
		t.Fatalf("read: %d", resp.StatusCode)
	}
	resp, _ = http.Get(srv.URL + "/api/ci/status")
	var st StatusView
	_ = json.NewDecoder(resp.Body).Decode(&st)
	if st.Box != "box" || st.Queued != 1 {
		t.Fatalf("status %+v", st)
	}
}

func TestDispatchRefusedWhenDisabled(t *testing.T) {
	s := newTestService(t, "")
	s.Config.Enabled = false
	h := &Handler{Service: s, Token: "tok"}
	mux := http.NewServeMux()
	h.Register(mux, func(f http.HandlerFunc) http.HandlerFunc { return f })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	body, _ := json.Marshal(req("MyGame", KindCompile, 'a'))
	r, _ := http.NewRequest("POST", srv.URL+"/api/ci/jobs", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer tok")
	resp, _ := http.DefaultClient.Do(r)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("disabled box accepted a job: %d", resp.StatusCode)
	}
}

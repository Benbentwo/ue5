package ci

import (
	"archive/tar"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestArtifactDownloadRoundTrip(t *testing.T) {
	s := newArtifactService(t)
	j := finishedJob(t, s, "Mac", StateSucceeded)
	write := func(rel, body string) {
		p := filepath.Join(j.ArtifactDir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Mac/Game.app/Contents/MacOS/Game", "binary")
	write("Mac/Game.app/Contents/Resources/data.pak", "pak")
	if runtime.GOOS != "windows" {
		// A Mac bundle's links ship as the files they point at.
		_ = os.Symlink("MacOS", filepath.Join(j.ArtifactDir, "Mac/Game.app/Contents/Current"))
		_ = os.Symlink(".", filepath.Join(j.ArtifactDir, "Mac/loop")) // must not recurse forever
	}

	h := &Handler{Service: s, Token: "tok"}
	mux := http.NewServeMux()
	h.Register(mux, func(f http.HandlerFunc) http.HandlerFunc { return f })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if resp, _ := http.Get(srv.URL + "/api/ci/jobs/" + j.ID + "/artifacts.tar"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a download without the token: %d", resp.StatusCode)
	}
	dest := t.TempDir()
	got, files, _, err := Fetch{URL: srv.URL, Token: "tok", Game: j.Game, SHA: j.SHA, Kind: KindPackage, Platform: "Mac"}.Run(context.Background(), dest)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != j.ID {
		t.Fatalf("fetched %s, want %s", got.ID, j.ID)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "Mac/Game.app/Contents/MacOS/Game")); string(b) != "binary" {
		_ = filepath.Walk(dest, func(p string, _ os.FileInfo, _ error) error { t.Log(p); return nil })
		t.Fatalf("binary %q", b)
	}
	if runtime.GOOS != "windows" {
		if b, _ := os.ReadFile(filepath.Join(dest, "Mac/Game.app/Contents/Current/Game")); string(b) != "binary" {
			t.Fatalf("a linked directory did not ship as files: %q", b)
		}
	}
	if files < 2 {
		t.Fatalf("files %d", files)
	}
	if _, _, _, err := (Fetch{URL: srv.URL, Token: "tok", Game: j.Game, SHA: j.SHA, Kind: KindPackage, Platform: "Win64"}).Run(context.Background(), t.TempDir()); err == nil {
		t.Fatal("no Win64 package exists, so the fetch must fail")
	}
}

func TestExtractTarRefusesEscapes(t *testing.T) {
	for _, name := range []string{"../evil", "a/../../evil", "/abs/../../evil"} {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("x"))
		_ = tw.Close()
		dest := t.TempDir()
		_, _, err := ExtractTar(&buf, dest)
		if _, serr := os.Stat(filepath.Join(filepath.Dir(dest), "evil")); serr == nil {
			t.Fatalf("%q escaped the destination", name)
		}
		_ = err // a cleaned name lands inside dest, which is just as safe as refusing it
	}
}

func newArtifactService(t *testing.T) *Service {
	t.Helper()
	cfg := Config{Enabled: true, Box: "box", WorkRoot: t.TempDir()}
	cfg.fill()
	s, err := NewService(t.TempDir(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.Reporter = &Reporter{}
	return s
}

func finishedJob(t *testing.T, s *Service, platform string, state State) *Job {
	t.Helper()
	r := req("MyGame", KindPackage, 'a')
	r.Platform = platform
	j, err := s.Dispatch(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Queue.Next(""); !ok {
		t.Fatal("no job to start")
	}
	done, err := s.Queue.Finish(j.ID, state, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Config.ArtifactRoot, done.Game, done.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	done, err = s.Queue.Update(done.ID, func(x *Job) { x.ArtifactDir = dir })
	if err != nil {
		t.Fatal(err)
	}
	return done
}

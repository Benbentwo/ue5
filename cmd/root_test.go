package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectFileReturnsAbsolutePathForDot(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "Game.uproject")
	if err := os.WriteFile(want, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	for _, in := range []string{".", "./Game.uproject", "Game.uproject"} {
		got, err := resolveProjectFile(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if !filepath.IsAbs(got) {
			t.Fatalf("%q: got relative path %q", in, got)
		}
		gotReal, _ := filepath.EvalSymlinks(got)
		wantReal, _ := filepath.EvalSymlinks(want)
		if gotReal != wantReal {
			t.Fatalf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestResolveProjectFileNoUproject(t *testing.T) {
	if _, err := resolveProjectFile(t.TempDir()); err == nil {
		t.Fatal("expected error for folder with no .uproject")
	}
}

package ci

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Artifact downloads let one box use what another box built, e.g. a store upload
// on one box that ships packages built on two. Packages are not public, so these
// routes need the serving box's dispatch token, unlike the other reads.

// ArtifactFile is one file in a job's artifact directory.
type ArtifactFile struct {
	Path string `json:"path"` // slash-separated, relative to the artifact directory
	Size int64  `json:"size"`
}

// artifactDir returns a finished job's artifact directory, or an HTTP status and reason.
func (h *Handler) artifactDir(id string) (*Job, string, int, string) {
	j, err := h.Service.Queue.Get(id)
	if err != nil {
		return nil, "", http.StatusNotFound, err.Error()
	}
	if j.State == StateQueued || j.State == StateRunning {
		return nil, "", http.StatusConflict, "job " + id + " has not finished"
	}
	if j.ArtifactDir == "" {
		return nil, "", http.StatusNotFound, "job " + id + " kept no artifacts"
	}
	fi, err := os.Stat(j.ArtifactDir)
	if err != nil || !fi.IsDir() {
		return nil, "", http.StatusNotFound, "job " + id + " has no artifacts on disk (none were written, or they were pruned)"
	}
	return j, j.ArtifactDir, 0, ""
}

func (h *Handler) artifactList(w http.ResponseWriter, r *http.Request) {
	j, dir, code, why := h.artifactDir(r.PathValue("id"))
	if j == nil {
		writeErr(w, code, why)
		return
	}
	files := []ArtifactFile{}
	err := walkArtifacts(dir, func(rel, _ string, fi fs.FileInfo) error {
		files = append(files, ArtifactFile{Path: rel, Size: fi.Size()})
		return nil
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": h.view(j), "files": files})
}

// artifactTar streams the artifact directory as an uncompressed tar.
func (h *Handler) artifactTar(w http.ResponseWriter, r *http.Request) {
	j, dir, code, why := h.artifactDir(r.PathValue("id"))
	if j == nil {
		writeErr(w, code, why)
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", j.Game+"-"+j.Platform+"-"+j.ID+".tar"))
	tw := tar.NewWriter(w)
	err := walkArtifacts(dir, func(rel, full string, fi fs.FileInfo) error {
		if err := r.Context().Err(); err != nil {
			return err
		}
		hdr := &tar.Header{Name: rel, Mode: int64(fi.Mode().Perm()), Size: fi.Size(), ModTime: fi.ModTime(), Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(full)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = io.CopyN(tw, f, fi.Size())
		return err
	})
	if err != nil {
		// Headers are gone; a truncated tar is what the client sees, and it fails on it.
		h.Service.Logf("ci: artifact download of %s stopped: %v", j.ID, err)
		return
	}
	_ = tw.Close()
}

// walkArtifacts visits every regular file under root, following symlinks (a Mac
// app bundle has them, and a receiving Windows box cannot make them). A link to
// a directory already open further up the walk is skipped, so a link loop
// cannot run forever; a link to a sibling directory ships that directory twice.
func walkArtifacts(root string, fn func(rel, full string, fi fs.FileInfo) error) error {
	seen := map[string]bool{}
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return err
		}
		if seen[real] {
			return nil
		}
		seen[real] = true
		defer delete(seen, real)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			full := filepath.Join(dir, e.Name())
			name := path.Join(rel, e.Name())
			fi, err := os.Stat(full) // follows links
			if err != nil {
				continue // a dangling link: nothing to ship
			}
			switch {
			case fi.IsDir():
				if err := walk(full, name); err != nil {
					return err
				}
			case fi.Mode().IsRegular():
				if err := fn(name, full, fi); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root, "")
}

// ExtractTar writes a tar stream under dest, refusing any entry that would land
// outside it. Only regular files and directories are written. It returns the
// number of files and bytes written.
func ExtractTar(r io.Reader, dest string) (int, int64, error) {
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return 0, 0, err
	}
	tr := tar.NewReader(r)
	files, total := 0, int64(0)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files, total, nil
		}
		if err != nil {
			return files, total, fmt.Errorf("reading the archive (it may have been cut off): %w", err)
		}
		clean := path.Clean("/" + hdr.Name)[1:]
		if clean == "" || strings.HasPrefix(clean, "../") {
			return files, total, fmt.Errorf("refusing archive entry %q", hdr.Name)
		}
		target := filepath.Join(destAbs, filepath.FromSlash(clean))
		if !strings.HasPrefix(target, destAbs+string(filepath.Separator)) {
			return files, total, fmt.Errorf("refusing archive entry %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return files, total, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return files, total, err
			}
			mode := os.FileMode(hdr.Mode).Perm() | 0o600
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return files, total, err
			}
			n, err := io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return files, total, err
			}
			_ = os.Chtimes(target, hdr.ModTime, hdr.ModTime)
			files++
			total += n
		}
	}
}

// Fetch finds the newest succeeded job on a remote build service matching game,
// sha, kind and platform, and extracts its artifacts into dest.
type Fetch struct {
	URL      string // the remote service, e.g. http://buildbox.local:9516
	Token    string // the REMOTE box's dispatch token
	Game     string
	SHA      string
	Kind     Kind
	Platform string
	Client   *http.Client
}

// Run returns the job it fetched and the file count and byte total written.
func (f Fetch) Run(ctx context.Context, dest string) (*Job, int, int64, error) {
	base := strings.TrimRight(f.URL, "/")
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 0} // packages are large; the context bounds the call
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/ci/jobs?limit=500&game="+url.QueryEscape(f.Game), nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	var list struct {
		Jobs []*Job `json:"jobs"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&list)
	_ = resp.Body.Close()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("listing jobs on %s: %w", base, err)
	}
	var pick *Job
	for _, j := range list.Jobs { // newest first
		if j.Kind == f.Kind && j.Platform == f.Platform && j.SHA == f.SHA && j.State == StateSucceeded {
			pick = j
			break
		}
	}
	if pick == nil {
		return nil, 0, 0, fmt.Errorf("%s has no succeeded %s %s job of %s at %s", base, f.Game, f.Kind, f.Platform, f.SHA)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/ci/jobs/"+url.PathEscape(pick.ID)+"/artifacts.tar", nil)
	req.Header.Set("Authorization", "Bearer "+f.Token)
	resp, err = client.Do(req)
	if err != nil {
		return pick, 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return pick, 0, 0, fmt.Errorf("downloading %s from %s: HTTP %d: %s", pick.ID, base, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	n, total, err := ExtractTar(resp.Body, dest)
	if err == nil && total == 0 {
		err = fmt.Errorf("job %s on %s returned an empty archive", pick.ID, base)
	}
	return pick, n, total, err
}

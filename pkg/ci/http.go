package ci

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Handler serves the build API under /api/ci/. Reads are open to the LAN (the
// dashboard is meant to be viewed from any machine in the house); anything
// that starts, cancels or retries a build needs the box's dispatch token.
type Handler struct {
	Service *Service
	Token   string
}

// Register mounts the routes. wrap adds the dashboard's CORS headers.
func (h *Handler) Register(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/ci/status", wrap(h.status))
	mux.HandleFunc("GET /api/ci/jobs", wrap(h.list))
	mux.HandleFunc("GET /api/ci/jobs/{id}", wrap(h.get))
	mux.HandleFunc("GET /api/ci/jobs/{id}/log", wrap(h.log))
	mux.HandleFunc("POST /api/ci/jobs", wrap(h.auth(h.dispatch)))
	mux.HandleFunc("POST /api/ci/jobs/{id}/cancel", wrap(h.auth(h.cancel)))
	mux.HandleFunc("POST /api/ci/jobs/{id}/retry", wrap(h.auth(h.retry)))
}

func (h *Handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if h.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(h.Token)) != 1 {
			writeErr(w, http.StatusUnauthorized, "a valid dispatch token is required (Authorization: Bearer <~/.ue5/ci/token>)")
			return
		}
		next(w, r)
	}
}

// StatusView is GET /api/ci/status: what the dashboard header shows per box.
type StatusView struct {
	Box         string   `json:"box"`
	Enabled     bool     `json:"enabled"`
	PublicURL   string   `json:"public_url"`
	Running     *Job     `json:"running,omitempty"`
	Queued      int      `json:"queued"`
	FreeGB      int      `json:"free_gb"`
	DiskFloorGB int      `json:"disk_floor_gb"`
	WorkRoot    string   `json:"work_root"`
	Peers       []string `json:"peers"`
	Reporting   bool     `json:"reporting"` // an Azure PAT is configured
	// ReportError is the last commit-status failure, e.g. a PAT Azure rejects.
	ReportError string `json:"report_error,omitempty"`
	Now         string `json:"now"`
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	s := h.Service
	free, _ := FreeGB(existingParent(s.Config.WorkRoot))
	peers := s.Config.Peers
	if peers == nil {
		peers = []string{}
	}
	writeJSON(w, http.StatusOK, StatusView{
		Box: s.Config.Box, Enabled: s.Config.Enabled, PublicURL: s.Config.PublicURL,
		Running: s.Running(), Queued: len(s.Queue.Queued()), FreeGB: free,
		DiskFloorGB: s.Config.DiskFloorGB, WorkRoot: s.Config.WorkRoot, Peers: peers,
		Reporting: s.Reporter.HasPAT(), ReportError: s.Reporter.LastError(), Now: time.Now().UTC().Format(time.RFC3339),
	})
}

// JobView adds what only the queue knows to a job.
type JobView struct {
	*Job
	Position    int    `json:"position,omitempty"`
	DurationSec int    `json:"duration_sec"`
	URL         string `json:"url"`
}

func (h *Handler) view(j *Job) JobView {
	v := JobView{Job: j, DurationSec: int(j.Duration(time.Now()).Seconds()), URL: h.Service.Config.PublicURL + "/#/ci/" + j.ID}
	if j.State == StateQueued {
		v.Position = h.Service.Queue.Position(j.ID)
	}
	return v
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	game := r.URL.Query().Get("game")
	jobs := h.Service.Queue.List(0)
	out := make([]JobView, 0, limit)
	for _, j := range jobs {
		if game != "" && j.Game != game {
			continue
		}
		out = append(out, h.view(j))
		if len(out) == limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"box": h.Service.Config.Box, "jobs": out})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	j, err := h.Service.Queue.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.view(j))
}

// log returns the job log as text. ?offset=N returns bytes from N on, so the
// dashboard can tail a running build by polling with the length it already has;
// the X-Log-Size header carries the new length.
func (h *Handler) log(w http.ResponseWriter, r *http.Request) {
	j, err := h.Service.Queue.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Access-Control-Expose-Headers", "X-Log-Size")
	if j.LogPath == "" {
		w.Header().Set("X-Log-Size", "0")
		return
	}
	f, err := os.Open(j.LogPath)
	if err != nil {
		w.Header().Set("X-Log-Size", "0")
		return
	}
	defer func() { _ = f.Close() }()
	fi, _ := f.Stat()
	size := fi.Size()
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if tail, _ := strconv.ParseInt(r.URL.Query().Get("tail"), 10, 64); tail > 0 && offset == 0 && size > tail {
		offset = size - tail
	}
	if offset < 0 || offset > size {
		offset = size
	}
	w.Header().Set("X-Log-Size", strconv.FormatInt(size, 10))
	_, _ = f.Seek(offset, io.SeekStart)
	_, _ = io.CopyN(w, f, size-offset)
}

func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request) {
	if !h.Service.Config.Enabled {
		writeErr(w, http.StatusServiceUnavailable, "the build service is off on "+h.Service.Config.Box+` (set "enabled": true in ~/.ue5/ci/config.json)`)
		return
	}
	var req Request
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return
	}
	if req.Branch != "" {
		req.Branch = strings.TrimPrefix(req.Branch, "refs/heads/")
	}
	j, err := h.Service.Dispatch(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, h.view(j))
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	j, err := h.Service.Cancel(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, h.view(j))
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request) {
	j, err := h.Service.Queue.Retry(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	h.Service.Reporter.Report(r.Context(), j)
	writeJSON(w, http.StatusAccepted, h.view(j))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// existingParent is path, or its nearest ancestor that exists (the work root
// may not be created until the first sync).
func existingParent(path string) string {
	for p := path; ; p = filepath.Dir(p) {
		if _, err := os.Stat(p); err == nil || filepath.Dir(p) == p {
			return p
		}
	}
}

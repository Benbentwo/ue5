package ci

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AzureRepo is an Azure Repos git URL taken apart.
type AzureRepo struct {
	Base    string // https://dev.azure.com/<org>
	Project string // unescaped, e.g. "My Project"
	Repo    string // unescaped, e.g. "My Game"
}

// ParseAzureRepo accepts the URLs Azure hands a pipeline
// ($(Build.Repository.Uri)), with or without user info:
//
//	https://dev.azure.com/Org/Project/_git/Repo
//	https://Org@dev.azure.com/Org/Project%20Name/_git/Repo%20Name
//	https://org.visualstudio.com/Project/_git/Repo
func ParseAzureRepo(raw string) (AzureRepo, bool) {
	u, err := url.Parse(strings.TrimSuffix(raw, ".git"))
	if err != nil || u.Host == "" {
		return AzureRepo{}, false
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	gi := -1
	for i, p := range parts {
		if p == "_git" {
			gi = i
		}
	}
	if gi < 1 || gi+1 >= len(parts) {
		return AzureRepo{}, false
	}
	unesc := func(s string) string {
		v, err := url.PathUnescape(s)
		if err != nil {
			return s
		}
		return v
	}
	var base string
	var project string
	switch {
	case u.Host == "dev.azure.com" && gi == 2:
		base = "https://dev.azure.com/" + parts[0]
		project = unesc(parts[1])
	case strings.HasSuffix(u.Host, ".visualstudio.com") && gi == 1:
		base = "https://" + u.Host
		project = unesc(parts[0])
	default:
		return AzureRepo{}, false
	}
	return AzureRepo{Base: base, Project: project, Repo: unesc(parts[gi+1])}, true
}

// Reporter posts a job's state as a commit status on Azure Repos, so the
// commit shows the real build result beside the dispatch run's "handed off".
type Reporter struct {
	PAT       string
	PublicURL string
	Client    *http.Client
	// Errorf receives failures; reporting is best effort and never fails a job.
	Errorf func(format string, args ...any)
}

// azureState maps job states onto Azure's GitStatusState.
func azureState(s State) string {
	switch s {
	case StateQueued, StateRunning:
		return "pending"
	case StateSucceeded:
		return "succeeded"
	case StateFailed:
		return "failed"
	}
	return "notApplicable" // superseded, canceled, interrupted
}

// StatusName is the check's name on the commit, e.g. "ue5-ci/package-Win64".
func StatusName(j *Job) string { return string(j.Kind) + "-" + j.Platform }

// Report posts one status for j. A repo that is not on Azure, or no PAT, is a
// silent no-op.
func (r *Reporter) Report(ctx context.Context, j *Job) {
	if r == nil || r.PAT == "" {
		return
	}
	repo, ok := ParseAzureRepo(j.Repo)
	if !ok {
		return
	}
	desc := describe(j)
	body := map[string]any{
		"state":       azureState(j.State),
		"description": desc,
		"targetUrl":   r.PublicURL + "/#/ci/" + j.ID,
		"context":     map[string]string{"name": StatusName(j), "genre": "ue5-ci"},
	}
	buf, _ := json.Marshal(body)
	endpoint := fmt.Sprintf("%s/%s/_apis/git/repositories/%s/commits/%s/statuses?api-version=7.1",
		repo.Base, url.PathEscape(repo.Project), url.PathEscape(repo.Repo), j.SHA)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		r.errorf("commit status: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+r.PAT)))
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		r.errorf("commit status for %s: %v", j.ID, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		r.errorf("commit status for %s: HTTP %d: %s", j.ID, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
}

func (r *Reporter) errorf(format string, args ...any) {
	if r.Errorf != nil {
		r.Errorf(format, args...)
	}
}

// describe is the one-line status text Azure shows beside the check.
func describe(j *Job) string {
	where := " on " + j.Box
	switch j.State {
	case StateQueued:
		return "queued" + where
	case StateRunning:
		return "building" + where
	case StateSucceeded:
		return "succeeded" + where + " in " + j.Duration(time.Now()).String()
	case StateFailed:
		if len(j.Errors) > 0 {
			return trim("failed: "+j.Errors[0], 250)
		}
		if j.Message != "" {
			return trim("failed: "+j.Message, 250)
		}
		return "failed" + where
	}
	if j.Message != "" {
		return trim(string(j.State)+": "+j.Message, 250)
	}
	return string(j.State)
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

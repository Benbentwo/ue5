package ci

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseAzureRepo(t *testing.T) {
	cases := map[string]AzureRepo{
		"https://dev.azure.com/ExampleOrg/My%20Project/_git/My%20Game":         {"https://dev.azure.com/ExampleOrg", "My Project", "My Game"},
		"https://ExampleOrg@dev.azure.com/ExampleOrg/My%20Project/_git/MyGame": {"https://dev.azure.com/ExampleOrg", "My Project", "MyGame"},
		"https://dev.azure.com/ExampleOrg/My%20Project/_git/OtherGame.git":     {"https://dev.azure.com/ExampleOrg", "My Project", "OtherGame"},
		"https://exampleorg.visualstudio.com/My%20Project/_git/ThirdGame":      {"https://exampleorg.visualstudio.com", "My Project", "ThirdGame"},
	}
	for in, want := range cases {
		got, ok := ParseAzureRepo(in)
		if !ok || got != want {
			t.Errorf("%s: got %+v %v, want %+v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"https://github.com/Benbentwo/ue5", "not a url", "https://dev.azure.com/Org/_git"} {
		if _, ok := ParseAzureRepo(bad); ok {
			t.Errorf("%s: expected not-azure", bad)
		}
	}
}

func TestCleanRepoURL(t *testing.T) {
	if got := cleanRepoURL("https://Org@dev.azure.com/Org/P/_git/R"); got != "https://dev.azure.com/Org/P/_git/R" {
		t.Fatal(got)
	}
	if got := cleanRepoURL("https://dev.azure.com/Org/P/_git/R@x"); got != "https://dev.azure.com/Org/P/_git/R@x" {
		t.Fatal(got)
	}
}

func TestReporterPostsCommitStatus(t *testing.T) {
	var gotPath, gotAuth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath() + "?" + r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		w.WriteHeader(201)
	}))
	defer srv.Close()

	j := &Job{ID: "j1", Box: "build-mac", State: StateFailed, Errors: []string{"Foo.cpp(172,46): error C4458"},
		Request: Request{Game: "MyGame", Kind: KindCompile, Platform: "Mac", SHA: sha('a'),
			Repo: "https://dev.azure.com/ExampleOrg/My%20Project/_git/MyGame"}}
	// Point the parsed base at the test server.
	rep := &Reporter{PAT: "secret", PublicURL: "http://build-mac.local:9516", Client: &http.Client{Transport: rewrite(srv.URL)}}
	rep.Report(context.Background(), j)

	wantPath := "/ExampleOrg/My%20Project/_apis/git/repositories/MyGame/commits/" + sha('a') + "/statuses?api-version=7.1"
	if gotPath != wantPath {
		t.Fatalf("path %s\nwant %s", gotPath, wantPath)
	}
	if gotAuth != "Basic "+base64.StdEncoding.EncodeToString([]byte(":secret")) {
		t.Fatalf("auth %q", gotAuth)
	}
	if body["state"] != "failed" || !strings.Contains(body["description"].(string), "C4458") {
		t.Fatalf("body %+v", body)
	}
	if body["targetUrl"] != "http://build-mac.local:9516/#/ci/j1" {
		t.Fatalf("targetUrl %v", body["targetUrl"])
	}
	ctx := body["context"].(map[string]any)
	if ctx["name"] != "compile-Mac" || ctx["genre"] != "ue5-ci" {
		t.Fatalf("context %+v", ctx)
	}
}

func TestReporterWithoutPATIsSilent(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	(&Reporter{Client: &http.Client{Transport: rewrite(srv.URL)}}).Report(context.Background(),
		&Job{Request: Request{Repo: "https://dev.azure.com/O/P/_git/R", SHA: sha('a')}})
	if called {
		t.Fatal("no PAT must mean no request")
	}
}

// rewrite sends every request to target, keeping the path.
type rewrite string

func (t rewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	u := *r.URL
	base := strings.TrimPrefix(string(t), "http://")
	u.Scheme, u.Host = "http", base
	r2 := r.Clone(r.Context())
	r2.URL = &u
	r2.Host = base
	return http.DefaultTransport.RoundTrip(r2)
}

func TestAzureSignInPageIsARejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusNonAuthoritativeInfo) // what Azure sends for a PAT it does not accept
		_, _ = w.Write([]byte("<html>sign in</html>"))
	}))
	defer srv.Close()
	rep := &Reporter{PAT: "bad", Client: &http.Client{Transport: rewrite(srv.URL)}}
	rep.Report(context.Background(), &Job{ID: "j", State: StateFailed,
		Request: Request{Repo: "https://dev.azure.com/O/P/_git/R", SHA: sha('a'), Kind: KindCompile, Platform: "Mac"}})
	if !strings.Contains(rep.LastError(), "did not accept the PAT") {
		t.Fatalf("a 203 must be reported as a rejected PAT, got %q", rep.LastError())
	}
	if _, err := CheckAuth(context.Background(), &http.Client{Transport: rewrite(srv.URL)}, "https://dev.azure.com/O", "bad"); err == nil {
		t.Fatal("CheckAuth must reject a 203")
	}
}

func TestCheckAuthAccepts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"authenticatedUser":{"providerDisplayName":"Build Bot"}}`))
	}))
	defer srv.Close()
	who, err := CheckAuth(context.Background(), &http.Client{Transport: rewrite(srv.URL)}, "https://dev.azure.com/O", "good")
	if err != nil || who != "Build Bot" {
		t.Fatalf("got %q %v", who, err)
	}
}

func TestAuthFailureMatchesGitPromptErrors(t *testing.T) {
	for _, l := range []string{
		"fatal: could not read Username for 'https://dev.azure.com': Device not configured",
		"fatal: could not read Username for 'https://dev.azure.com': terminal prompts disabled",
		"remote: TF401019 ... fatal: Authentication failed for 'https://dev.azure.com/O/P/_git/R/'",
	} {
		if !authFailure(l) {
			t.Errorf("not recognised: %s", l)
		}
	}
	if authFailure("fatal: couldn't find remote ref deadbeef") {
		t.Error("a missing ref is not an auth failure")
	}
}

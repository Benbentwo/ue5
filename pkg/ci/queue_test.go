package ci

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sha(c byte) string { return strings.Repeat(string(c), 40) }

func req(game string, kind Kind, c byte) Request {
	return Request{Game: game, Kind: kind, Platform: "Win64", Repo: "https://dev.azure.com/O/P/_git/" + game, SHA: sha(c)}
}

func newTestQueue(t *testing.T) (*Queue, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.json")
	q, err := NewQueue(path, "box")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	q.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	return q, path
}

func TestEnqueueSupersedesQueuedTwin(t *testing.T) {
	q, _ := newTestQueue(t)
	a, _, _ := q.Enqueue(req("MyGame", KindCompile, 'a'))
	other, _, _ := q.Enqueue(req("OtherGame", KindCompile, 'b'))
	b, sup, err := q.Enqueue(req("MyGame", KindCompile, 'c'))
	if err != nil {
		t.Fatal(err)
	}
	if len(sup) != 1 || sup[0].ID != a.ID || sup[0].SupersededBy != b.ID {
		t.Fatalf("expected %s superseded by %s, got %+v", a.ID, b.ID, sup)
	}
	got, _ := q.Get(a.ID)
	if got.State != StateSuperseded {
		t.Fatalf("old twin is %s", got.State)
	}
	if o, _ := q.Get(other.ID); o.State != StateQueued {
		t.Fatalf("a different game must not be superseded, got %s", o.State)
	}
	// A different kind of the same game is a different key.
	_, sup, _ = q.Enqueue(req("MyGame", KindPackage, 'd'))
	if len(sup) != 0 {
		t.Fatalf("package must not supersede compile: %+v", sup)
	}
}

func TestRunningJobIsNotSuperseded(t *testing.T) {
	q, _ := newTestQueue(t)
	a, _, _ := q.Enqueue(req("MyGame", KindCompile, 'a'))
	if j, ok := q.Next(""); !ok || j.ID != a.ID {
		t.Fatal("expected a to start")
	}
	_, sup, _ := q.Enqueue(req("MyGame", KindCompile, 'b'))
	if len(sup) != 0 {
		t.Fatal("a running build must finish; only queued ones are superseded")
	}
}

func TestNextRunsCompileBeforePackageThenOldestFirst(t *testing.T) {
	q, _ := newTestQueue(t)
	pkg, _, _ := q.Enqueue(req("MyGame", KindPackage, 'a'))
	steam, _, _ := q.Enqueue(req("FourthGame", KindSteam, 'b'))
	c1, _, _ := q.Enqueue(req("OtherGame", KindCompile, 'c'))
	c2, _, _ := q.Enqueue(req("ThirdGame", KindCompile, 'd'))
	var order []string
	for {
		j, ok := q.Next("")
		if !ok {
			break
		}
		order = append(order, j.ID)
		if _, err := q.Finish(j.ID, StateSucceeded, nil, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{c1.ID, c2.ID, pkg.ID, steam.ID}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v, want %v", order, want)
	}
}

func TestHoldKeepsJobQueuedWithReason(t *testing.T) {
	q, _ := newTestQueue(t)
	a, _, _ := q.Enqueue(req("MyGame", KindCompile, 'a'))
	if _, ok := q.Next("waiting for disk"); ok {
		t.Fatal("a hold must not start a job")
	}
	got, _ := q.Get(a.ID)
	if got.State != StateQueued || got.Message != "waiting for disk" {
		t.Fatalf("got %s %q", got.State, got.Message)
	}
	if j, ok := q.Next(""); !ok || j.Message != "" {
		t.Fatal("the job should start, with the hold message cleared, once the hold lifts")
	}
}

func TestRestartTurnsRunningIntoInterruptedPlusRetry(t *testing.T) {
	q, path := newTestQueue(t)
	a, _, _ := q.Enqueue(req("MyGame", KindCompile, 'a'))
	q.Next("")
	q2, err := NewQueue(path, "box")
	if err != nil {
		t.Fatal(err)
	}
	old, _ := q2.Get(a.ID)
	if old.State != StateInterrupted {
		t.Fatalf("running job after restart is %s, want interrupted", old.State)
	}
	queued := q2.Queued()
	if len(queued) != 1 || queued[0].RetryOf != a.ID || queued[0].SHA != a.SHA {
		t.Fatalf("expected one retry of %s, got %+v", a.ID, queued)
	}
}

func TestCancel(t *testing.T) {
	q, _ := newTestQueue(t)
	a, _, _ := q.Enqueue(req("MyGame", KindCompile, 'a'))
	b, _, _ := q.Enqueue(req("OtherGame", KindCompile, 'b'))
	if j, _ := q.Cancel(b.ID); j.State != StateCanceled {
		t.Fatalf("queued cancel gave %s", j.State)
	}
	q.Next("")
	j, err := q.Cancel(a.ID)
	if err != nil || j.State != StateRunning || !j.CancelRequested {
		t.Fatalf("running cancel: %+v %v", j, err)
	}
	if _, err := q.Cancel(b.ID); err == nil {
		t.Fatal("cancelling a finished job must fail")
	}
}

func TestValidateRejectsBadRequests(t *testing.T) {
	good := req("MyGame", KindCompile, 'a')
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Request){
		"no game":      func(r *Request) { r.Game = "" },
		"path in game": func(r *Request) { r.Game = "../etc" },
		"bad kind":     func(r *Request) { r.Kind = "deploy" },
		"short sha":    func(r *Request) { r.SHA = "abc" },
		"no repo":      func(r *Request) { r.Repo = "" },
		"bad plugin":   func(r *Request) { r.Plugins = map[string]Pinned{"X": {Repo: "r", SHA: "zz"}} },
	} {
		r := good
		mut(&r)
		if r.Validate() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestTokenPathPrecedenceAndSharing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("UE5_CI_TOKEN_FILE", "")
	if got := TokenPath(home, ""); got != filepath.Join(home, "ci", "token") {
		t.Fatalf("default %s", got)
	}
	shared := filepath.Join(t.TempDir(), "shared", "token")
	t.Setenv("UE5_CI_TOKEN_FILE", shared)
	if got := TokenPath(home, ""); got != shared {
		t.Fatalf("env %s", got)
	}
	if got := TokenPath(home, "/explicit"); got != "/explicit" {
		t.Fatalf("explicit %s", got)
	}
	// The daemon creates the shared token; another account reading the same file gets the same value.
	a, err := DispatchTokenAt(shared)
	if err != nil || len(a) < 32 {
		t.Fatalf("create %q %v", a, err)
	}
	if b, _ := DispatchTokenAt(shared); b != a {
		t.Fatal("a second reader must get the same token")
	}
}

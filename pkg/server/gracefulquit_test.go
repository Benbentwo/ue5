package server

import (
	"encoding/json"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBridge is a TCP server speaking the SadTire MCP bridge wire protocol:
// one JSON request per connection, one JSON response back.
type fakeBridge struct {
	t        *testing.T
	listener net.Listener
	port     int

	mu          sync.Mutex
	calls       []string // command, or "editor_exec:<console command>"
	pid         int
	pieRunning  bool
	pieEndAfter int // pie_status polls after stop_pie before PIE reports ended
	execOpID    int
	execBusy    int // editor_exec dispatches to reject as "already in progress"
	pyHandled   bool
	onClean     func() // the clean-close snippet ran (nil = it does nothing)
	onQuit      func()
}

func newFakeBridge(t *testing.T) *fakeBridge {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	b := &fakeBridge{t: t, listener: l, port: l.Addr().(*net.TCPAddr).Port, pid: 4242, pyHandled: true}
	go b.serve()
	t.Cleanup(func() { _ = l.Close() })
	return b
}

func (b *fakeBridge) serve() {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer func() { _ = c.Close() }()
			var req struct {
				Command string                 `json:"command"`
				Params  map[string]interface{} `json:"params"`
			}
			if err := json.NewDecoder(c).Decode(&req); err != nil {
				return
			}
			_ = json.NewEncoder(c).Encode(b.handle(req.Command, req.Params))
		}(conn)
	}
}

func ok(result map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"status": "success", "result": result}
}

func fail(msg string) map[string]interface{} {
	return map[string]interface{}{"status": "error", "error": msg}
}

func (b *fakeBridge) handle(command string, params map[string]interface{}) map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch command {
	case "ping":
		b.calls = append(b.calls, command)
		return ok(map[string]interface{}{"message": "pong", "pid": b.pid})
	case "pie_status":
		b.calls = append(b.calls, command)
		return ok(map[string]interface{}{"is_running": b.pieRunning})
	case "stop_pie":
		b.calls = append(b.calls, command)
		if b.pieEndAfter == 0 {
			b.pieRunning = false
		}
		return ok(map[string]interface{}{"dispatched": true})
	case "editor_exec":
		console, _ := params["command"].(string)
		if b.execBusy > 0 {
			b.execBusy--
			b.calls = append(b.calls, "editor_exec(busy)")
			return fail("A deferred editor_exec is already in progress")
		}
		b.calls = append(b.calls, "editor_exec:"+console)
		b.execOpID++
		if console == cleanCloseCommand && b.pyHandled && b.onClean != nil {
			go b.onClean()
		}
		if console == quitEditorCommand && b.onQuit != nil {
			go b.onQuit()
		}
		return ok(map[string]interface{}{"dispatched": true, "op_id": b.execOpID})
	case "editor_exec_status":
		b.calls = append(b.calls, command)
		if got := jsonInt(params["op_id"]); got != b.execOpID {
			return fail("not the current op")
		}
		return ok(map[string]interface{}{"done": true, "result": map[string]interface{}{"handled": b.pyHandled}})
	}
	return fail("Unknown command: " + command)
}

// pieStatusTick lets a test end PIE a few polls after stop_pie.
func (b *fakeBridge) endPIEAfterPolls(n int) {
	b.mu.Lock()
	b.pieEndAfter = n
	b.mu.Unlock()
	go func() {
		for {
			time.Sleep(5 * time.Millisecond)
			b.mu.Lock()
			stopped := false
			polls := 0
			for _, c := range b.calls {
				if c == "stop_pie" {
					stopped = true
				} else if stopped && c == "pie_status" {
					polls++
				}
			}
			if polls >= n {
				b.pieRunning = false
				b.mu.Unlock()
				return
			}
			b.mu.Unlock()
		}
	}()
}

func (b *fakeBridge) callLog() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

func (b *fakeBridge) count(prefix string) int {
	n := 0
	for _, c := range b.callLog() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func fastTimings(t *testing.T) {
	t.Helper()
	orig := gracefulTimings
	gracefulTimings = gracefulQuitTimings{
		BootWait:    600 * time.Millisecond,
		HungWait:    100 * time.Millisecond,
		BootHorizon: 10 * time.Minute,
		PIEStop:     time.Second,
		CleanExit:   300 * time.Millisecond,
		Exit:        time.Second,
		Call:        500 * time.Millisecond,
		PingCall:    300 * time.Millisecond,
		Poll:        10 * time.Millisecond,
	}
	t.Cleanup(func() { gracefulTimings = orig })
}

func indexOf(calls []string, want string) int {
	for i, c := range calls {
		if c == want {
			return i
		}
	}
	return -1
}

func TestGracefulQuitEndsPIEThenCleanCloses(t *testing.T) {
	fastTimings(t)
	b := newFakeBridge(t)
	b.pieRunning = true
	b.endPIEAfterPolls(3)

	var gone atomic.Bool
	b.onClean = func() { time.Sleep(30 * time.Millisecond); gone.Store(true) }

	if !gracefulQuitEditor(b.port, b.pid, time.Now(), gone.Load) {
		t.Fatalf("expected a clean self-quit; calls: %v", b.callLog())
	}

	calls := b.callLog()
	stop := indexOf(calls, "stop_pie")
	clean := indexOf(calls, "editor_exec:"+cleanCloseCommand)
	if calls[0] != "ping" || stop < 0 || clean < 0 {
		t.Fatalf("missing a step; calls: %v", calls)
	}
	if stop >= clean {
		t.Errorf("clean close (%d) must come after stop_pie (%d): %v", clean, stop, calls)
	}
	// PIE must be OBSERVED ended before the editor is asked to close.
	polls := 0
	for _, c := range calls[stop:clean] {
		if c == "pie_status" {
			polls++
		}
	}
	if polls < 3 {
		t.Errorf("expected to poll pie_status until PIE ended (>=3 polls), got %d: %v", polls, calls)
	}
	if n := b.count("editor_exec:" + quitEditorCommand); n != 0 {
		t.Errorf("a clean close that worked must not be followed by QUIT_EDITOR (%d sent): %v", n, calls)
	}
}

func TestCleanCloseCommandIsOneConsoleLine(t *testing.T) {
	// The whole snippet travels as one console command: a raw newline would
	// end it early, and '|' is the console's command separator.
	if strings.ContainsAny(cleanCloseCommand, "\n\r|") {
		t.Errorf("clean close command is not a single safe console line: %q", cleanCloseCommand)
	}
	if !strings.HasPrefix(cleanCloseCommand, `py exec("import unreal\n`) {
		t.Errorf("unexpected shape: %q", cleanCloseCommand)
	}
	for _, want := range []string{"CLOSE_SLATE_MAINFRAME", "QUIT_EDITOR", "get_dirty_content_packages", "get_dirty_map_packages"} {
		if !strings.Contains(cleanCloseCommand, want) {
			t.Errorf("clean close command lost %q", want)
		}
	}
}

func TestGracefulQuitSendsPlainQuitWhenCleanCloseDoesNothing(t *testing.T) {
	fastTimings(t)
	b := newFakeBridge(t) // onClean nil: the snippet raised, or the close was refused
	var gone atomic.Bool
	b.onQuit = func() { gone.Store(true) }

	start := time.Now()
	if !gracefulQuitEditor(b.port, b.pid, time.Now(), gone.Load) {
		t.Fatalf("expected the plain QUIT_EDITOR to finish the job: %v", b.callLog())
	}
	if time.Since(start) < gracefulTimings.CleanExit {
		t.Errorf("QUIT_EDITOR was sent before the clean close had its %s", gracefulTimings.CleanExit)
	}
	calls := b.callLog()
	if c, q := indexOf(calls, "editor_exec:"+cleanCloseCommand), indexOf(calls, "editor_exec:"+quitEditorCommand); c < 0 || q < c {
		t.Errorf("want clean close then QUIT_EDITOR, got %v", calls)
	}
}

func TestGracefulQuitSkipsStopPIEWhenNotPlaying(t *testing.T) {
	fastTimings(t)
	b := newFakeBridge(t)
	var gone atomic.Bool
	b.onClean = func() { gone.Store(true) }

	if !gracefulQuitEditor(b.port, b.pid, time.Now(), gone.Load) {
		t.Fatalf("expected a clean self-quit; calls: %v", b.callLog())
	}
	if b.count("stop_pie") != 0 {
		t.Errorf("stop_pie sent with no PIE running: %v", b.callLog())
	}
}

func TestGracefulQuitRefusesAForeignBridge(t *testing.T) {
	fastTimings(t)
	b := newFakeBridge(t)
	b.pid = 111 // some other editor owns this port

	if gracefulQuitEditor(b.port, 222, time.Now(), func() bool { return false }) {
		t.Fatal("must not report a quit when the bridge belongs to another process")
	}
	if n := b.count("editor_exec"); n != 0 {
		t.Errorf("a foreign editor was sent %d editor_exec command(s): %v", n, b.callLog())
	}
}

func TestGracefulQuitWaitsForBootThenGivesUp(t *testing.T) {
	fastTimings(t)
	// A port nothing listens on: the editor is still booting.
	l, _ := net.Listen("tcp4", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	start := time.Now()
	if gracefulQuitEditor(port, 1, time.Now(), func() bool { return false }) {
		t.Fatal("expected false when the bridge never comes up")
	}
	waited := time.Since(start)
	if waited < gracefulTimings.BootWait-50*time.Millisecond {
		t.Errorf("gave up after %s, before the %s boot-wait bound — would signal a booting editor", waited, gracefulTimings.BootWait)
	}
	if waited > gracefulTimings.BootWait+2*time.Second {
		t.Errorf("waited %s, far past the %s bound", waited, gracefulTimings.BootWait)
	}
}

func TestGracefulQuitOldSilentEditorGetsTheShortBound(t *testing.T) {
	fastTimings(t)
	l, _ := net.Listen("tcp4", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	start := time.Now()
	// Started an hour ago: a silent bridge is a hang, not a boot.
	if gracefulQuitEditor(port, 1, time.Now().Add(-time.Hour), func() bool { return false }) {
		t.Fatal("expected false")
	}
	if waited := time.Since(start); waited > gracefulTimings.BootWait-100*time.Millisecond {
		t.Errorf("an old editor waited %s — it should get HungWait (%s), not BootWait", waited, gracefulTimings.HungWait)
	}
}

func TestGracefulQuitBridgeComesUpMidWait(t *testing.T) {
	fastTimings(t)
	gracefulTimings.BootWait = 3 * time.Second
	// Reserve a port, release it, and bring the bridge up on it later.
	l, _ := net.Listen("tcp4", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	var gone atomic.Bool
	go func() {
		time.Sleep(200 * time.Millisecond)
		l2, err := net.Listen("tcp4", "127.0.0.1:"+itoa(port))
		if err != nil {
			return
		}
		b := &fakeBridge{t: t, listener: l2, port: port, pid: 7, pyHandled: true}
		b.onClean = func() { gone.Store(true) }
		t.Cleanup(func() { _ = l2.Close() })
		b.serve()
	}()

	if !gracefulQuitEditor(port, 7, time.Now(), gone.Load) {
		t.Fatal("expected the quit to go through once the bridge came up")
	}
}

func TestGracefulQuitRetriesABusyExecSlot(t *testing.T) {
	fastTimings(t)
	b := newFakeBridge(t)
	b.execBusy = 2
	var gone atomic.Bool
	b.onClean = func() { gone.Store(true) }

	if !gracefulQuitEditor(b.port, b.pid, time.Now(), gone.Load) {
		t.Fatalf("expected quit after the exec slot freed; calls: %v", b.callLog())
	}
}

func TestGracefulQuitUnhandledPythonGoesStraightToQuit(t *testing.T) {
	fastTimings(t)
	gracefulTimings.CleanExit = 5 * time.Second // must NOT be waited out
	b := newFakeBridge(t)
	b.pyHandled = false // project without the Python plugin: `py` is unhandled
	b.onClean = func() { t.Error("an unhandled snippet cannot have run") }
	var gone atomic.Bool
	b.onQuit = func() { gone.Store(true) }

	start := time.Now()
	if !gracefulQuitEditor(b.port, b.pid, time.Now(), gone.Load) {
		t.Fatalf("QUIT_EDITOR must still happen without Python: %v", b.callLog())
	}
	if waited := time.Since(start); waited > 2*time.Second {
		t.Errorf("waited %s on a snippet the editor reported as unhandled", waited)
	}
}

func TestGracefulQuitReportsFalseWhenEditorIgnoresQuit(t *testing.T) {
	fastTimings(t)
	gracefulTimings.Exit = 150 * time.Millisecond
	b := newFakeBridge(t)

	if gracefulQuitEditor(b.port, b.pid, time.Now(), func() bool { return false }) {
		t.Fatal("expected false when the process never exits, so the caller escalates")
	}
	if b.count("editor_exec:"+quitEditorCommand) != 1 {
		t.Errorf("QUIT_EDITOR should have been dispatched exactly once: %v", b.callLog())
	}
}

func TestGracefulQuitKeepsWaitingWhenTheBridgeIsAlreadyGone(t *testing.T) {
	// A slow teardown: the clean close was accepted, the bridge closed, and
	// the process is still unwinding when CleanExit runs out. The QUIT_EDITOR
	// dispatch fails (nothing listening) — that must not be read as "give up
	// and signal", which would cut a clean exit short.
	fastTimings(t)
	gracefulTimings.CleanExit = 100 * time.Millisecond
	gracefulTimings.Call = 100 * time.Millisecond
	b := newFakeBridge(t)
	var gone atomic.Bool
	b.onClean = func() {
		_ = b.listener.Close()
		time.Sleep(400 * time.Millisecond)
		gone.Store(true)
	}

	if !gracefulQuitEditor(b.port, b.pid, time.Now(), gone.Load) {
		t.Fatal("an editor mid-teardown must be waited for, not handed to SIGTERM")
	}
}

func TestGracefulQuitNoPortNeverGuesses(t *testing.T) {
	called := false
	orig := bridgeCall
	bridgeCall = func(int, string, map[string]interface{}, time.Duration) (map[string]interface{}, error) {
		called = true
		return nil, nil
	}
	defer func() { bridgeCall = orig }()

	if gracefulQuitEditor(0, 1, time.Now(), func() bool { return false }) {
		t.Fatal("expected false with no port")
	}
	if called {
		t.Error("port 0 must not fall back to a guessed bridge port")
	}
}

func TestMCPPortFromArgs(t *testing.T) {
	cases := map[string]int{
		testEditorBinary + " " + testProjectPath + " -SadTireMCPPort=55561":        55561,
		testEditorBinary + " " + testProjectPath + " -SadTireMCPPort=55560 -log":   55560,
		testEditorBinary + " " + testProjectPath:                                   0,
		testEditorBinary + " " + testProjectPath + " -NotSadTireMCPPort=55561":     0,
		testEditorBinary + " " + testProjectPath + " -SadTireMCPPort=abc":          0,
		testEditorBinary + " " + testProjectPath + " -SadTireMCPPort=99999999":     0,
		testEditorBinary + " /p/x-SadTireMCPPort=1.uproject -SadTireMCPPort=55570": 55570,
	}
	for args, want := range cases {
		if got := mcpPortFromArgs(args); got != want {
			t.Errorf("mcpPortFromArgs(%q) = %d, want %d", args, got, want)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// --- StopEditor: graceful first, signals only as the fallback ---------------

func startSleeper(t *testing.T) (*InstanceManager, *ProjectInstance, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sleep and SIGTERM")
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	path := "/fake/Game/Game.uproject"
	inst := &ProjectInstance{
		Info: InstanceInfo{ProjectPath: path, ProjectName: "Game", State: StateRunning,
			PID: cmd.Process.Pid, MCPPort: 55599, StartedAt: time.Now()},
		cmd:  cmd,
		done: make(chan struct{}),
	}
	m := NewInstanceManager()
	m.instances[path] = inst
	go m.monitorProcess(inst)
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return m, inst, path
}

func stubGracefulQuit(t *testing.T, fn func(port, pid int, startedAt time.Time, exited func() bool) bool) {
	t.Helper()
	orig := gracefulQuit
	gracefulQuit = fn
	t.Cleanup(func() { gracefulQuit = orig })
}

func TestStopEditorAsksEditorToQuitBeforeSignalling(t *testing.T) {
	m, inst, path := startSleeper(t)

	var gotPort, gotPID int
	stubGracefulQuit(t, func(port, pid int, _ time.Time, exited func() bool) bool {
		gotPort, gotPID = port, pid
		// Stand in for the editor quitting by itself. SIGKILL so that a
		// SIGTERM from StopEditor could not be mistaken for the cause.
		_ = inst.cmd.Process.Kill()
		for !exited() {
			time.Sleep(5 * time.Millisecond)
		}
		return true
	})

	info, err := m.StopEditor(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if gotPort != 55599 || gotPID != inst.Info.PID {
		t.Errorf("graceful quit got port=%d pid=%d, want 55599/%d", gotPort, gotPID, inst.Info.PID)
	}
	if info.State != StateStopped {
		t.Errorf("state = %s, want stopped (a requested stop is not a crash)", info.State)
	}
}

func TestStopEditorFallsBackToSIGTERM(t *testing.T) {
	m, _, path := startSleeper(t)
	called := false
	stubGracefulQuit(t, func(int, int, time.Time, func() bool) bool { called = true; return false })

	start := time.Now()
	info, err := m.StopEditor(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("graceful quit was not attempted")
	}
	if info.State != StateStopped {
		t.Errorf("state = %s, want stopped after the SIGTERM fallback", info.State)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("fallback took %s", time.Since(start))
	}
}

func TestStopEditorForceSkipsGracefulQuit(t *testing.T) {
	m, inst, path := startSleeper(t)
	stubGracefulQuit(t, func(int, int, time.Time, func() bool) bool {
		t.Error("force=true must not wait on the bridge")
		return false
	})

	if _, err := m.StopEditor(path, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inst.done:
	case <-time.After(5 * time.Second):
		t.Fatal("force kill did not end the process")
	}
}

// --- Process gate: untracked editors get the same courtesy -------------------

func TestEnsureNoEditorProcessesAsksUntrackedEditorToQuit(t *testing.T) {
	alive := true
	origScan := scanEditorProcesses
	scanEditorProcesses = func(editorBinary, projectPath string) ([]editorProc, []editorProc, error) {
		if !alive {
			return nil, nil, nil
		}
		return []editorProc{
			{PID: 2147480001, Args: editorBinary + " " + projectPath + " -SadTireMCPPort=55577"},
		}, nil, nil
	}
	defer func() { scanEditorProcesses = origScan }()

	var gotPort, gotPID int
	stubGracefulQuit(t, func(port, pid int, _ time.Time, _ func() bool) bool {
		gotPort, gotPID = port, pid
		alive = false // it quit by itself
		return true
	})

	if err := ensureNoEditorProcesses(testEditorBinary, testProjectPath); err != nil {
		t.Fatalf("expected the gate to pass after a graceful quit, got %v", err)
	}
	if gotPort != 55577 || gotPID != 2147480001 {
		t.Errorf("graceful quit got port=%d pid=%d, want 55577/2147480001", gotPort, gotPID)
	}
}

func TestEnsureNoEditorProcessesNeverGuessesAPort(t *testing.T) {
	scans := 0
	origScan := scanEditorProcesses
	scanEditorProcesses = func(editorBinary, projectPath string) ([]editorProc, []editorProc, error) {
		scans++
		if scans > 1 {
			return nil, nil, nil
		}
		// A human-launched editor: no -SadTireMCPPort on its command line.
		return []editorProc{{PID: 2147480002, Args: editorBinary + " " + projectPath}}, nil, nil
	}
	defer func() { scanEditorProcesses = origScan }()

	stubGracefulQuit(t, func(int, int, time.Time, func() bool) bool {
		t.Error("an editor with no port on its command line must not be probed over a guessed port")
		return false
	})

	if err := ensureNoEditorProcesses(testEditorBinary, testProjectPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseEtime(t *testing.T) {
	cases := map[string]time.Duration{
		"00:07":      7 * time.Second,
		"12:34":      12*time.Minute + 34*time.Second,
		"01:02:03":   time.Hour + 2*time.Minute + 3*time.Second,
		"2-01:02:03": 49*time.Hour + 2*time.Minute + 3*time.Second,
		"  05:00  ":  5 * time.Minute,
		"":           0,
		"garbage":    0,
		"x-01:02:03": 0,
	}
	for in, want := range cases {
		if got := parseEtime(in); got != want {
			t.Errorf("parseEtime(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestQuitEditorGracefullyNeverGuessesAPort(t *testing.T) {
	_, inst, _ := startSleeper(t) // a live process with no -SadTireMCPPort
	stubGracefulQuit(t, func(int, int, time.Time, func() bool) bool {
		t.Error("must not probe a bridge for a process with no port")
		return false
	})
	exited, err := QuitEditorGracefully(inst.Info.PID, 0, 0)
	if exited || err == nil {
		t.Errorf("want exited=false and an error, got exited=%v err=%v", exited, err)
	}
}

func TestQuitEditorGracefullyDeadPidIsAlreadyDone(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip("no `true` binary")
	}
	exited, err := QuitEditorGracefully(cmd.Process.Pid, 0, 0)
	if !exited || err != nil {
		t.Errorf("a dead pid is already stopped: exited=%v err=%v", exited, err)
	}
}

func TestGracefulQuitGivesUpFastWhenTheEngineTickIsBlocked(t *testing.T) {
	// A modal dialog: the bridge answers, the clean close is accepted but
	// never runs, and the exec slot stays busy. The long Exit wait would be
	// wasted on an editor that is provably not exiting.
	fastTimings(t)
	gracefulTimings.Exit = 30 * time.Second
	b := newFakeBridge(t)
	b.mu.Lock()
	b.execBusy = 0
	b.mu.Unlock()
	orig := bridgeCall
	seenClean := false
	bridgeCall = func(port int, command string, params map[string]interface{}, timeout time.Duration) (map[string]interface{}, error) {
		if command == "editor_exec" {
			if c, _ := params["command"].(string); c == quitEditorCommand && seenClean {
				return nil, bridgeRefusal("A deferred editor_exec is already in progress (queued)")
			}
			seenClean = true
		}
		if command == "editor_exec_status" {
			return map[string]interface{}{"done": false, "state": "queued"}, nil
		}
		return orig(port, command, params, timeout)
	}
	defer func() { bridgeCall = orig }()

	start := time.Now()
	if gracefulQuitEditor(b.port, b.pid, time.Now(), func() bool { return false }) {
		t.Fatal("expected false so the caller escalates")
	}
	if waited := time.Since(start); waited > 5*time.Second {
		t.Errorf("waited %s on an editor whose bridge refused the quit", waited)
	}
}

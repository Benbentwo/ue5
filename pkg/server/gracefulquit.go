package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// Why this file exists.
//
// On macOS a SIGTERM lands in the engine's GracefulTerminationHandler, which
// only calls RequestEngineExit: the main loop falls straight into engine
// teardown with whatever the editor had open. A Play-In-Editor session that
// is still up, an open asset editor, or an editor that has not finished
// booting each crash that teardown (~FSlateApplication with PIE viewports
// alive, the Niagara world-cleanup fatal, ~ULocalPlayer subsystem asserts,
// ~FAnimationEditor, UContentBrowserFileDataSource::Shutdown), and the
// handler itself is not async-signal-safe. Every one of those leaves a crash
// report and pops the interactive crash reporter.
//
// So before the daemon signals an editor it now asks the editor to quit by
// itself, over the SadTire MCP bridge the daemon already allocated a port
// for (-SadTireMCPPort). The sequence is:
//
//  1. wait until the bridge answers `ping` (it gates every command on the
//     editor's first real tick, so a pong means boot is complete) and check
//     the pong's pid is the process we mean to stop;
//  2. if PIE is running, `stop_pie` and poll `pie_status` until it has ended;
//  3. run the editor's own File > Quit path (CLOSE_SLATE_MAINFRAME ->
//     FMainFrameHandler::ShutDownEditor), which closes every asset editor
//     and the root window before it queues QUIT_EDITOR. That path opens a
//     modal save prompt when packages are dirty, which nothing here could
//     answer, so a small Python snippet in the editor picks: nothing dirty
//     -> CLOSE_SLATE_MAINFRAME; anything dirty -> plain QUIT_EDITOR
//     (UUnrealEdEngine::CloseEditor: no prompt, the same "unsaved work is
//     dropped" contract SIGTERM had);
//  4. wait for the process to exit; if it has not started exiting, send a
//     plain QUIT_EDITOR as a second request and wait again.
//
// There is deliberately no separate "close all asset editors" command:
// UAssetEditorSubsystem::CloseAllAssetEditors is not exposed to Python in
// 5.8 (measured: AttributeError, and `py` still reports handled), and no
// console command reaches it. The main-frame close is the only script-
// reachable route that closes them, which is why step 3 prefers it.
//
// Every step is bounded. Any failure or timeout returns false and the caller
// falls through to the SIGTERM -> SIGKILL ladder it always had, so a hung or
// never-booting editor still cannot hold a rebuild.

// gracefulQuitTimings bounds each phase of the bridge-driven quit.
type gracefulQuitTimings struct {
	// BootWait bounds waiting for a pong from an editor that may still be
	// booting (bridge not listening yet, or gating commands on first tick).
	BootWait time.Duration
	// HungWait replaces BootWait for an editor old enough that "still
	// booting" is no longer a plausible reason for a silent bridge.
	HungWait time.Duration
	// BootHorizon is the age past which an editor is no longer assumed to
	// be booting.
	BootHorizon time.Duration
	// PIEStop bounds waiting for PIE to actually end after stop_pie.
	PIEStop time.Duration
	// CleanExit bounds waiting for the process to exit after the clean
	// close request, before a plain QUIT_EDITOR is sent as well.
	CleanExit time.Duration
	// Exit bounds waiting for the process to exit after QUIT_EDITOR.
	Exit time.Duration
	// Call is the socket timeout for one ordinary bridge command.
	Call time.Duration
	// PingCall is the socket timeout for one boot-wait ping. The bridge
	// holds a ping for up to ~30s while its boot gate retries, so this is
	// longer than Call to let one connection ride the gate out.
	PingCall time.Duration
	// Poll is the interval between status polls.
	Poll time.Duration
}

// gracefulTimings is a var (not const) so tests can shrink it.
var gracefulTimings = gracefulQuitTimings{
	BootWait:    180 * time.Second,
	HungWait:    15 * time.Second,
	BootHorizon: 10 * time.Minute,
	PIEStop:     20 * time.Second,
	CleanExit:   20 * time.Second,
	Exit:        45 * time.Second,
	Call:        5 * time.Second,
	PingCall:    35 * time.Second,
	Poll:        250 * time.Millisecond,
}

const quitEditorCommand = "QUIT_EDITOR"

// cleanCloseScript runs inside the editor's Python. It chooses between the
// editor's real File > Quit path and the prompt-free QUIT_EDITOR; see the
// file comment. An unreadable dirty list counts as dirty.
const cleanCloseScript = `import unreal
def _count(fn):
    try:
        return len(fn())
    except Exception:
        return 1
_u = unreal.EditorLoadingAndSavingUtils
_dirty = _count(_u.get_dirty_content_packages) + _count(_u.get_dirty_map_packages)
try:
    _confirm = bool(unreal.EditorPerProjectUserSettings.get_default_object().get_editor_property('confirm_editor_close'))
except Exception:
    _confirm = False
_cmd = 'QUIT_EDITOR' if (_dirty or _confirm) else 'CLOSE_SLATE_MAINFRAME'
unreal.log('ue5 graceful stop: %d dirty package(s), confirm_editor_close=%s -> %s' % (_dirty, _confirm, _cmd))
unreal.SystemLibrary.execute_console_command(None, _cmd)
`

// cleanCloseCommand is cleanCloseScript as one console line. strconv.Quote
// yields a double-quoted, backslash-escaped ASCII literal that Python reads
// back as the same text, so the script can keep its newlines.
var cleanCloseCommand = "py exec(" + strconv.Quote(cleanCloseScript) + ")"

// bridgeCallFn sends one command to the SadTire MCP bridge on 127.0.0.1:port
// and returns the `result` object of a success response.
type bridgeCallFn func(port int, command string, params map[string]interface{}, timeout time.Duration) (map[string]interface{}, error)

// bridgeCall is the bridge seam; swappable in tests.
var bridgeCall bridgeCallFn = sendBridgeCommand

// gracefulQuit is the whole-sequence seam; swappable in tests so the stop
// paths can be exercised without a bridge.
var gracefulQuit = gracefulQuitEditor

// sendBridgeCommand speaks the bridge's wire protocol: one connection per
// command, a single JSON object {"command","params"} out, a single JSON
// object {"status","result"|"error"} back.
func sendBridgeCommand(port int, command string, params map[string]interface{}, timeout time.Duration) (map[string]interface{}, error) {
	if params == nil {
		params = map[string]interface{}{}
	}
	conn, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	payload, err := json.Marshal(map[string]interface{}{"command": command, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(payload); err != nil {
		return nil, err
	}

	var resp struct {
		Status string                 `json:"status"`
		Error  string                 `json:"error"`
		Result map[string]interface{} `json:"result"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, err
	}
	if resp.Status != "success" {
		if resp.Error == "" {
			resp.Error = "bridge returned status " + strconv.Quote(resp.Status)
		}
		return nil, bridgeRefusal(resp.Error)
	}
	if resp.Result == nil {
		resp.Result = map[string]interface{}{}
	}
	return resp.Result, nil
}

// bridgeRefusal is an error the bridge itself answered with, as opposed to a
// transport failure. The difference matters once a quit is under way: a
// refusal means the editor is alive and answering (so it is not exiting),
// while a dead socket is what a normal teardown looks like.
type bridgeRefusal string

func (e bridgeRefusal) Error() string { return string(e) }

var mcpPortArgRe = regexp.MustCompile(`(?:^|\s)-SadTireMCPPort=(\d+)(?:\s|$)`)

// mcpPortFromArgs extracts the -SadTireMCPPort value from an editor command
// line, or 0 when it carries none. Zero means "do not guess": the bridge's
// compiled-in default port may belong to another project's editor.
func mcpPortFromArgs(args string) int {
	m := mcpPortArgRe.FindStringSubmatch(args)
	if m == nil {
		return 0
	}
	port, err := strconv.Atoi(m[1])
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

// gracefulQuitEditor drives the editor on `port` to a clean self-exit.
//
// pid is the process expected behind the port (0 skips the identity check);
// startedAt is when it was launched (zero = unknown, treated as possibly
// still booting); exited reports whether the process is gone. Returns true
// only when the process has exited. On false nothing has been signalled and
// the caller owns the escalation.
func gracefulQuitEditor(port, pid int, startedAt time.Time, exited func() bool) bool {
	if exited() {
		return true
	}
	if port == 0 {
		return false
	}
	t := gracefulTimings
	logger := log.With("pid", pid, "mcp_port", port)

	// --- 1. boot-ready + identity ------------------------------------------
	bootWait := t.BootWait
	if !startedAt.IsZero() && time.Since(startedAt) > t.BootHorizon {
		bootWait = t.HungWait
	}
	bootDeadline := time.Now().Add(bootWait)
	announced := false
	for {
		callTimeout := t.PingCall
		if remaining := time.Until(bootDeadline); remaining < callTimeout {
			callTimeout = remaining
		}
		if callTimeout <= 0 {
			logger.Warn("Editor bridge not ready within the boot-wait bound; falling back to signals", "waited", bootWait)
			return false
		}
		pong, err := bridgeCall(port, "ping", nil, callTimeout)
		if exited() {
			return true
		}
		if err == nil {
			if got := jsonInt(pong["pid"]); pid != 0 && got != 0 && got != pid {
				logger.Warn("Bridge on this port belongs to a different process; not asking it to quit", "bridge_pid", got)
				return false
			}
			break
		}
		if !announced {
			logger.Info("Editor bridge not answering yet; waiting for boot-ready before stopping", "bound", bootWait, "error", err)
			announced = true
		}
		sleepUntil(t.Poll*4, bootDeadline)
	}

	// --- 2. end PIE ---------------------------------------------------------
	if status, err := bridgeCall(port, "pie_status", nil, t.Call); err == nil && jsonBool(status["is_running"]) {
		logger.Info("PIE is running; requesting end play before quitting the editor")
		if _, err := bridgeCall(port, "stop_pie", nil, t.Call); err != nil {
			logger.Warn("stop_pie failed; QUIT_EDITOR will end play itself", "error", err)
		} else {
			ended := pollUntil(t.PIEStop, t.Poll, exited, func() bool {
				s, err := bridgeCall(port, "pie_status", nil, t.Call)
				return err == nil && !jsonBool(s["is_running"])
			})
			if !ended {
				logger.Warn("PIE did not end within bound; QUIT_EDITOR will end play itself", "bound", t.PIEStop)
			}
		}
	}
	if exited() {
		return true
	}

	// --- 3. clean close: the editor's own File > Quit when nothing is dirty ---
	opID, err := bridgeExecDispatch(port, cleanCloseCommand, t.Call, exited)
	if err != nil {
		logger.Warn("Could not dispatch the clean close; trying a plain QUIT_EDITOR", "error", err)
	} else {
		// The bridge dies with the editor, so the exec's own status is only
		// consulted to cut the wait short when the snippet provably did not
		// run (a project without the Python plugin leaves `py` unhandled).
		notRun := func() bool {
			s, err := bridgeCall(port, "editor_exec_status", map[string]interface{}{"op_id": opID}, t.Call)
			if err != nil || !jsonBool(s["done"]) {
				return false
			}
			r, _ := s["result"].(map[string]interface{})
			return r != nil && !jsonBool(r["handled"])
		}
		if pollUntil(t.CleanExit, t.Poll, notRun, exited) {
			logger.Info("Editor quit by itself after bridge request")
			return true
		}
		logger.Warn("Editor has not exited after the clean close request; sending a plain QUIT_EDITOR")
	}
	if exited() {
		return true
	}

	// --- 4. plain QUIT_EDITOR, then wait ------------------------------------
	// A dispatch that fails on the socket is not the end: an editor already
	// deep in teardown has closed its bridge, and signalling it now would turn
	// a clean exit into an abrupt one, so keep waiting. A dispatch the bridge
	// REFUSES is the opposite case: the editor is alive and answering but the
	// exec slot is still held by the clean close it never ran, i.e. its
	// engine tick is blocked (a modal dialog). Waiting cannot help that.
	if _, err := bridgeExecDispatch(port, quitEditorCommand, t.Call, exited); err != nil {
		var refused bridgeRefusal
		if errors.As(err, &refused) {
			logger.Warn("Editor is answering but will not run QUIT_EDITOR (engine tick blocked?); falling back to signals", "error", err)
			return false
		}
		logger.Warn("Could not reach the bridge to send QUIT_EDITOR; waiting for exit anyway", "error", err)
	}
	if pollUntil(t.Exit, t.Poll, nil, exited) {
		logger.Info("Editor quit by itself after bridge request")
		return true
	}
	logger.Warn("Editor did not exit after QUIT_EDITOR within bound; falling back to signals", "bound", t.Exit)
	return false
}

// bridgeExecDispatch queues a console command on the bridge's deferred
// editor_exec slot and returns its op id. The slot holds one op, so a busy
// slot is retried inside the bound. It does not wait for the command to run:
// the commands sent here take the bridge down with the editor.
func bridgeExecDispatch(port int, command string, bound time.Duration, exited func() bool) (int, error) {
	t := gracefulTimings
	deadline := time.Now().Add(bound)
	for {
		dispatched, err := bridgeCall(port, "editor_exec", map[string]interface{}{"command": command}, t.Call)
		if err == nil {
			return jsonInt(dispatched["op_id"]), nil
		}
		if exited() || !time.Now().Before(deadline) {
			return 0, err
		}
		sleepUntil(t.Poll, deadline)
	}
}

// pollUntil calls cond every interval until it returns true or bound
// elapses. abort (optional) ends the wait early with false.
func pollUntil(bound, interval time.Duration, abort func() bool, cond func() bool) bool {
	deadline := time.Now().Add(bound)
	for {
		if cond() {
			return true
		}
		if abort != nil && abort() {
			return false
		}
		if !time.Now().Before(deadline) {
			return false
		}
		sleepUntil(interval, deadline)
	}
}

// sleepUntil sleeps d, clipped so it never overshoots deadline.
func sleepUntil(d time.Duration, deadline time.Time) {
	if remaining := time.Until(deadline); remaining < d {
		d = remaining
	}
	if d > 0 {
		time.Sleep(d)
	}
}

func jsonBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}

func jsonInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}

// QuitEditorGracefully asks the editor process pid to quit by itself over
// its SadTire MCP bridge, without going through the daemon. It is the same
// sequence StopEditor runs, exposed for tooling that has to stop an editor
// the daemon does not track (`ue5 server quit-editor`). port 0 means "read
// -SadTireMCPPort from the process's command line"; a process that carries
// none is never probed on a guessed port. It sends no signal: on false the
// caller owns the escalation. bootWait > 0 replaces the default bound on
// waiting for the bridge to answer (both the booting and the hung case) for
// callers that already know the process is not mid-boot. Not safe to call
// concurrently with a different bootWait: this is the one-shot CLI entry.
func QuitEditorGracefully(pid, port int, bootWait time.Duration) (bool, error) {
	if pid <= 0 {
		return false, errors.New("a positive pid is required")
	}
	if !processAlive(pid) {
		return true, nil
	}
	args, age := processArgsAndAge(pid)
	if port == 0 {
		port = mcpPortFromArgs(args)
	}
	if port == 0 {
		return false, fmt.Errorf("pid %d has no -SadTireMCPPort on its command line and none was given", pid)
	}
	var startedAt time.Time
	if age > 0 {
		startedAt = time.Now().Add(-age)
	}
	if bootWait > 0 {
		orig := gracefulTimings
		gracefulTimings.BootWait, gracefulTimings.HungWait = bootWait, bootWait
		defer func() { gracefulTimings = orig }()
	}
	exited := gracefulQuit(port, pid, startedAt, func() bool { return !processAlive(pid) })
	return exited, nil
}

// parseEtime parses ps's elapsed-time format, [[dd-]hh:]mm:ss.
func parseEtime(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var days int
	if i := strings.IndexByte(s, '-'); i >= 0 {
		d, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0
		}
		days = d
		s = s[i+1:]
	}
	total := 0
	for _, part := range strings.Split(s, ":") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0
		}
		total = total*60 + n
	}
	return time.Duration(days)*24*time.Hour + time.Duration(total)*time.Second
}

package server

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// fatalBuildMarkers are log substrings that mean the build did NOT produce
// binaries, even when the build script exited zero.
//
// WHY THIS EXISTS AT ALL
//
// `cmd.Wait()` reports the exit status of Epic's Build.sh, not of
// UnrealBuildTool. On 2026-09-01 UBT died with a segmentation fault inside
// libcoreclr, Build.sh still exited zero, and the daemon wrote
// `status=succeeded` into state.json for a build that relinked nothing. Every
// consumer of that status — the dispatcher's build poll, and any acceptance
// oracle that builds before it asserts — then ran against the PREVIOUS binary
// while believing it had just built the current source. A deliberately inverted
// tie-break was proven "passing" by two tests that had never been compiled.
//
// The transient segfault itself is the documented flaky-toolchain class and is
// cured by a retry. The bug is that the daemon called it a success.
//
// WHY SUBSTRINGS AND NOT AN EXIT CODE
//
// The exit code is the thing that lied. A log scan is the only evidence
// available once the process that lied has gone. These markers are chosen to be
// UBT's own failure prefixes, so a match means UBT itself declared a failure:
//
//   - "ERROR: " is what UBT prints for a fatal build error. It appears at the
//     start of a line, which is why matching is anchored — an "ERROR: " inside
//     a compiler diagnostic quoting source text must not count.
//   - "Segmentation fault" / "Bus error" are the crash lines from UBT's own
//     signal handler.
//
// A marker found alongside a NON-zero exit changes nothing: that build already
// failed. This scan only ever runs when the exit code claimed success, so
// anything it matches is by definition a false green.
var fatalBuildMarkers = []string{
	"ERROR: ",
	"Segmentation fault",
	"Bus error",
}

// maxScanBytes bounds the scan. A full rebuild log runs to tens of megabytes and
// this runs on the build's completion path, so the scan must not become the slow
// part of a build. UBT prints its fatal line and stops, so a failure is always
// near the END of the log — which is why the tail is what gets read.
const maxScanBytes = 512 * 1024

// scanFatalMarkers reports the first fatal marker in r, with the line carrying
// it. Returns empty strings when the log looks clean.
//
// Pure and reader-based so it can be tested without a build, a file, or a
// daemon — the property under test is "this text means failure", and nothing
// about that needs a process.
func scanFatalMarkers(r io.Reader) (marker string, line string) {
	scanner := bufio.NewScanner(r)
	// Build logs carry very long link command lines; the default 64 KiB token
	// limit would abort the scan partway with an error nobody checks, which is
	// the same silent-truncation failure this whole function exists to close.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		// LogCapture writes each line as "<rfc3339> <stream> | <raw>", so the
		// raw UBT text does NOT start the line on disk. Strip that prefix before
		// any anchored match -- without this, the "ERROR: " check can never fire
		// against a real captured log, and the scan would be dead code that
		// looked correct in a unit test fed bare text.
		text := scanner.Text()
		if idx := strings.Index(text, " | "); idx >= 0 {
			text = text[idx+3:]
		}
		trimmed := strings.TrimLeft(text, " \t")

		for _, m := range fatalBuildMarkers {
			// "ERROR: " must start the line. The crash markers may appear
			// anywhere, because UBT's handler prefixes them inconsistently.
			if m == "ERROR: " {
				if strings.HasPrefix(trimmed, m) {
					return strings.TrimSpace(m), strings.TrimSpace(text)
				}
				continue
			}
			if strings.Contains(text, m) {
				return m, strings.TrimSpace(text)
			}
		}
	}

	return "", ""
}

// scanBuildLogForFatal reads the tail of a build log and reports any fatal
// marker in it.
//
// A log that cannot be read returns NO marker: refusing to read a file is not
// evidence that a build failed, and turning an unreadable log into a build
// failure would break every build on a box with a permissions problem. The
// caller keeps whatever verdict the exit code gave it.
func scanBuildLogForFatal(path string) (marker string, line string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", ""
	}

	if info.Size() > maxScanBytes {
		if _, err := f.Seek(info.Size()-maxScanBytes, io.SeekStart); err != nil {
			return "", ""
		}
	}

	return scanFatalMarkers(f)
}

// fatalMarkerError is the error a false green becomes.
//
// It names the marker AND quotes the line, because the whole point is that the
// exit code said nothing useful — a bare "build failed" would send the reader
// back to the same log this function already read.
func fatalMarkerError(marker, line string) error {
	return fmt.Errorf(
		"build script exited zero but its log reports a fatal %s — no binaries were produced: %s",
		strings.TrimSpace(marker), line)
}

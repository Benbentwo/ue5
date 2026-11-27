package ci

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIsChildStartCode(t *testing.T) {
	cases := map[int]bool{
		0:                       false,
		1:                       false,
		0xC0000005:              false, // access violation, not a start failure
		int(int32(-1073741502)): true,  // 0xC0000142 as a signed exit status
		0xC0000142:              true,
	}
	for code, want := range cases {
		if got := isChildStartCode(code); got != want {
			t.Errorf("isChildStartCode(%#x) = %v, want %v", uint32(code), got, want)
		}
	}
}

func TestChildStartFailureIgnoresOtherErrors(t *testing.T) {
	if childStartFailure(nil) {
		t.Fatal("nil error reported as a start failure")
	}
	if childStartFailure(errors.New("git fetch: exit status 128")) {
		t.Fatal("a plain error reported as a start failure")
	}
}

func TestCheckChildStartPassesOnAHealthyHost(t *testing.T) {
	if err := checkChildStart(context.Background()); err != nil {
		t.Fatalf("probe failed on a healthy host: %v", err)
	}
}

func TestChildStartHintNamesTheStatus(t *testing.T) {
	if !strings.Contains(childStartHint, "0xC0000142") {
		t.Fatal("hint must name the status an operator will search for")
	}
}

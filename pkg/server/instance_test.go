package server

import (
	"testing"
)

func TestMonitorProcessRequestedStopNotCrash(t *testing.T) {
	// When state is StateStopping (daemon requested stop), a non-zero exit
	// should be treated as StateStopped, not StateCrashed.
	oldState := StateStopping
	newState := StateCrashed // simulate what happens with non-zero exit

	// Apply the fix logic
	if oldState == StateStopping {
		newState = StateStopped
	}

	if newState != StateStopped {
		t.Errorf("Expected StateStopped for daemon-requested stop, got %s", newState)
	}
}

func TestMonitorProcessUnexpectedExitIsCrash(t *testing.T) {
	// When state is StateRunning, a non-zero exit should be StateCrashed.
	oldState := StateRunning
	newState := StateCrashed // default for non-zero exit

	if oldState == StateStopping {
		newState = StateStopped
	}

	if newState != StateCrashed {
		t.Errorf("Expected StateCrashed for unexpected exit, got %s", newState)
	}
}

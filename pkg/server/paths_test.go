package server

import (
	"testing"
)

func TestDashboardAddr(t *testing.T) {
	t.Run("default port", func(t *testing.T) {
		// Ensure env var is unset for this test.
		t.Setenv("UE5_DASHBOARD_PORT", "")

		got := DashboardAddr()
		want := ":9516"
		if got != want {
			t.Errorf("DashboardAddr() = %q, want %q", got, want)
		}
	})

	t.Run("custom port from env", func(t *testing.T) {
		t.Setenv("UE5_DASHBOARD_PORT", "8080")

		got := DashboardAddr()
		want := ":8080"
		if got != want {
			t.Errorf("DashboardAddr() = %q, want %q", got, want)
		}
	})
}

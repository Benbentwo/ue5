//go:build !embed_dashboard

package server

import (
	"io/fs"
	"testing/fstest"
)

// dashboardStaticFS returns an empty FS in dev mode (no embedded static files).
func dashboardStaticFS() fs.FS {
	return fstest.MapFS{}
}

func hasDashboardStatic() bool {
	return false
}

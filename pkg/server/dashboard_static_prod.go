//go:build embed_dashboard

package server

import (
	"embed"
	"io/fs"
)

//go:embed dashboard_dist/*
var embeddedDashboard embed.FS

func dashboardStaticFS() fs.FS {
	sub, _ := fs.Sub(embeddedDashboard, "dashboard_dist")
	return sub
}

func hasDashboardStatic() bool {
	return true
}

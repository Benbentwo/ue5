#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "Building dashboard frontend..."
cd "$PROJECT_ROOT/dashboard"
npm ci
npm run build

echo "Copying dashboard dist to Go embed directory..."
rm -rf "$PROJECT_ROOT/pkg/server/dashboard_dist"
cp -r "$PROJECT_ROOT/dashboard/dist" "$PROJECT_ROOT/pkg/server/dashboard_dist"

echo "Building Go binary with embedded dashboard..."
cd "$PROJECT_ROOT"
go build -tags embed_dashboard -o ue5 .

echo "Done! Binary at: $PROJECT_ROOT/ue5"

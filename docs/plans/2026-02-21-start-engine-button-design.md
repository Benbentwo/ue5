# Start Engine Button

## Summary

Add a "Start Engine" button to the InstancePanel that launches the UE editor using the most recent successful build. The button is hidden when an editor is already running, and disabled when the latest build is in-progress or failed.

## Backend

**File:** `pkg/server/dashboard.go` — `handleEditorStart`

Make `engine_path` optional in the start request. If empty, resolve it using existing logic (check running instances, then parse `.uproject` file's `EngineAssociation`). This mirrors `builder.go:230-245`.

No new endpoints or types.

## Frontend

**File:** `dashboard/src/components/InstancePanel.tsx`

New props: `buildInfo: BuildInfo | null`, `selectedProject: string | null`

### Button visibility
- Hidden when any instance for the selected project is in `running` or `starting` state
- Shown otherwise (replaces the "No editor instances" empty state)

### Button state
- **Enabled:** most recent build status is `succeeded`
- **Disabled + reason:** build is `pending`/`building` ("Build in progress") or `failed` ("Last build failed") or no builds exist ("No builds yet")

### "Most recent build" resolution
- `buildInfo.current_build` if present, else `buildInfo.recent_builds[0]`

### Click action
- `POST /api/editor/start` with `{ project_path }` from the most recent succeeded build
- Loading state during request, error display on failure

**File:** `dashboard/src/App.tsx`

Pass `buildInfo` and `selectedProject` props to `InstancePanel`.

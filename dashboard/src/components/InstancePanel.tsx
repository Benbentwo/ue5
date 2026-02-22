import { useState } from 'react'
import type { InstanceInfo, BuildInfo, BuildRecord } from '../types'
import { postAPI } from '../hooks/useAPI'

interface InstancePanelProps {
  instances: InstanceInfo[]
  onAction: () => void
  buildInfo: BuildInfo | null
  selectedProject: string | null
}

function stateColor(state: InstanceInfo['state']): string {
  switch (state) {
    case 'running':
      return 'bg-green-400'
    case 'starting':
      return 'bg-yellow-400'
    case 'stopping':
      return 'bg-yellow-400'
    case 'stopped':
      return 'bg-gray-500'
    case 'crashed':
      return 'bg-red-400'
  }
}

function stateLabel(state: InstanceInfo['state']): string {
  return state.charAt(0).toUpperCase() + state.slice(1)
}

function latestBuild(
  buildInfo: BuildInfo | null,
  selectedProject: string | null,
): BuildRecord | null {
  if (buildInfo?.current_build) {
    if (!selectedProject || buildInfo.current_build.project_path === selectedProject)
      return buildInfo.current_build
  }
  if (buildInfo?.recent_builds?.length) {
    if (selectedProject) {
      const match = buildInfo.recent_builds.find(
        (b) => b.project_path === selectedProject,
      )
      return match ?? null
    }
    return buildInfo.recent_builds[0]
  }
  return null
}

function startDisabledReason(build: BuildRecord | null): string | null {
  if (!build) return 'No builds yet'
  switch (build.status) {
    case 'pending':
    case 'building':
      return 'Build in progress'
    case 'failed':
      return 'Last build failed'
    case 'succeeded':
      return null
  }
}

export default function InstancePanel({
  instances,
  onAction,
  buildInfo,
  selectedProject,
}: InstancePanelProps) {
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleStop(pid: number) {
    try {
      await postAPI(`/api/instances/${pid}/stop`, {})
      onAction()
    } catch {
      // Errors are non-critical for the UI
    }
  }

  const hasRunning = instances.some(
    (i) => i.state === 'running' || i.state === 'starting',
  )

  const build = latestBuild(buildInfo, selectedProject)
  const disabledReason = startDisabledReason(build)

  async function handleStart() {
    if (!build || disabledReason) return
    setStarting(true)
    setError(null)
    try {
      await postAPI('/api/editor/start', {
        project_path: build.project_path,
      })
      onAction()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start engine')
    } finally {
      setStarting(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-500">
        Editor Instances
      </h2>

      {instances.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {instances.map((inst) => (
            <li
              key={inst.pid}
              className="rounded-lg bg-gray-900 p-4 flex flex-col gap-2"
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span
                    className={`inline-flex h-2.5 w-2.5 rounded-full ${stateColor(inst.state)}`}
                  />
                  <span className="font-medium text-gray-200">
                    {inst.project_name}
                  </span>
                </div>
                {(inst.state === 'running' || inst.state === 'starting') && (
                  <button
                    onClick={() => handleStop(inst.pid)}
                    className="rounded bg-gray-800 px-2 py-1 text-xs text-red-400 hover:bg-gray-700 transition-colors"
                  >
                    Stop
                  </button>
                )}
              </div>
              <div className="text-sm text-gray-400 space-y-0.5">
                <p>State: {stateLabel(inst.state)}</p>
                <p>PID: {inst.pid}</p>
                <p>Engine: {inst.engine_version}</p>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <div className="rounded-lg bg-gray-900 p-4">
          <p className="text-sm text-gray-500">No editor instances</p>
        </div>
      )}

      {/* Start Engine button — shown only when no instance is running */}
      {!hasRunning && (
        <div className="flex flex-col gap-2">
          <button
            onClick={handleStart}
            disabled={!!disabledReason || starting}
            className="rounded-lg bg-green-600 px-4 py-2 text-sm font-medium text-white hover:bg-green-500 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
          >
            {starting ? 'Starting...' : 'Start Engine'}
          </button>
          {disabledReason && (
            <p className="text-xs text-gray-500">{disabledReason}</p>
          )}
          {error && <p className="text-xs text-red-400">{error}</p>}
        </div>
      )}
    </div>
  )
}

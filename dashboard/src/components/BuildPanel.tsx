import { useState } from 'react'
import type { BuildInfo, BuildRecord } from '../types'
import { postAPI } from '../hooks/useAPI'

interface BuildPanelProps {
  buildInfo: BuildInfo | null
  selectedProject: string | null
  onBuildTriggered: () => void
}

function StatusIcon({ status }: { status: BuildRecord['status'] }) {
  switch (status) {
    case 'succeeded':
      return <span className="text-green-400">&#10003;</span>
    case 'building':
    case 'pending':
      return <span className="text-yellow-400">&#9679;</span>
    case 'failed':
      return <span className="text-red-400">&#10007;</span>
  }
}

function statusColor(status: BuildRecord['status']): string {
  switch (status) {
    case 'succeeded':
      return 'text-green-400'
    case 'building':
    case 'pending':
      return 'text-yellow-400'
    case 'failed':
      return 'text-red-400'
  }
}

function timeAgo(dateStr: string): string {
  const seconds = Math.floor(
    (Date.now() - new Date(dateStr).getTime()) / 1000,
  )
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  return `${hours}h ago`
}

export default function BuildPanel({
  buildInfo,
  selectedProject,
  onBuildTriggered,
}: BuildPanelProps) {
  const [showForm, setShowForm] = useState(false)
  const [label, setLabel] = useState('')
  const [mode, setMode] = useState<'full' | 'hot_reload'>('full')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const current = buildInfo?.current_build

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      await postAPI('/api/rebuild', {
        label: label || 'manual',
        mode,
        project_path: selectedProject,
      })
      setShowForm(false)
      setLabel('')
      onBuildTriggered()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Rebuild failed')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-500">
        Build
      </h2>

      {/* Current build status */}
      <div className="rounded-lg bg-gray-900 p-4">
        {current ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <StatusIcon status={current.status} />
              <span className={`font-medium ${statusColor(current.status)}`}>
                {current.status.charAt(0).toUpperCase() +
                  current.status.slice(1)}
              </span>
            </div>
            <div className="text-sm text-gray-400 space-y-1">
              <p>Target: {current.target}</p>
              <p>Mode: {current.mode}</p>
              {current.labels.length > 0 && (
                <p>
                  Labels:{' '}
                  {current.labels.map((l) => (
                    <span
                      key={l}
                      className="inline-block rounded bg-gray-800 px-2 py-0.5 text-xs text-gray-300 mr-1"
                    >
                      {l}
                    </span>
                  ))}
                </p>
              )}
              {current.error && (
                <p className="text-red-400 text-xs mt-1">{current.error}</p>
              )}
            </div>
          </div>
        ) : (
          <p className="text-sm text-gray-500">No active build</p>
        )}
      </div>

      {/* Rebuild button / form */}
      {!showForm ? (
        <button
          onClick={() => setShowForm(true)}
          className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 transition-colors"
        >
          Rebuild
        </button>
      ) : (
        <form
          onSubmit={handleSubmit}
          className="rounded-lg bg-gray-900 p-4 flex flex-col gap-3"
        >
          <input
            type="text"
            placeholder="Label (e.g. feature-name)"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            className="rounded bg-gray-800 px-3 py-2 text-sm text-gray-200 border border-gray-700 focus:outline-none focus:border-blue-500"
          />
          <select
            value={mode}
            onChange={(e) => setMode(e.target.value as 'full' | 'hot_reload')}
            className="rounded bg-gray-800 px-3 py-2 text-sm text-gray-200 border border-gray-700 focus:outline-none focus:border-blue-500"
          >
            <option value="full">Full Build</option>
            <option value="hot_reload">Hot Reload</option>
          </select>
          {error && <p className="text-xs text-red-400">{error}</p>}
          <div className="flex gap-2">
            <button
              type="submit"
              disabled={submitting}
              className="flex-1 rounded bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-500 disabled:opacity-50 transition-colors"
            >
              {submitting ? 'Submitting...' : 'Submit'}
            </button>
            <button
              type="button"
              onClick={() => {
                setShowForm(false)
                setError(null)
              }}
              className="rounded bg-gray-800 px-3 py-2 text-sm text-gray-400 hover:text-gray-200 transition-colors"
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {/* Recent builds */}
      <div className="flex flex-col gap-2">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500">
          Recent Builds
        </h3>
        {buildInfo?.recent_builds && buildInfo.recent_builds.length > 0 ? (
          <ul className="flex flex-col gap-1">
            {buildInfo.recent_builds.slice(0, 10).map((b) => (
              <li
                key={b.id}
                className="flex items-center justify-between rounded bg-gray-900 px-3 py-2 text-sm"
              >
                <div className="flex items-center gap-2">
                  <StatusIcon status={b.status} />
                  <span className="text-gray-300">{b.mode}</span>
                  {b.labels.length > 0 && (
                    <span className="text-gray-500 text-xs">
                      {b.labels.join(', ')}
                    </span>
                  )}
                </div>
                <span className="text-gray-500 text-xs">
                  {timeAgo(b.started_at)}
                </span>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-gray-500">No recent builds</p>
        )}
        {buildInfo && (
          <p className="text-xs text-gray-500 mt-1">
            Total builds: {buildInfo.total_builds}
          </p>
        )}
      </div>
    </div>
  )
}

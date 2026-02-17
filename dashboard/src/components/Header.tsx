import type { DaemonStatus } from '../types'

interface HeaderProps {
  status: DaemonStatus | null
  projects: string[]
  selectedProject: string | null
  onSelectProject: (p: string | null) => void
}

function StatusDot({ building }: { building: boolean }) {
  if (building) {
    return (
      <span className="relative flex h-3 w-3">
        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-yellow-400 opacity-75" />
        <span className="relative inline-flex h-3 w-3 rounded-full bg-yellow-400" />
      </span>
    )
  }
  return <span className="inline-flex h-3 w-3 rounded-full bg-green-400" />
}

export default function Header({
  status,
  projects,
  selectedProject,
  onSelectProject,
}: HeaderProps) {
  return (
    <header className="flex items-center justify-between bg-gray-900 px-6 py-3 border-b border-gray-800">
      <div className="flex items-center gap-4">
        <h1 className="text-lg font-bold text-gray-100">UE5 Daemon</h1>
        {projects.length > 1 ? (
          <select
            value={selectedProject ?? ''}
            onChange={(e) =>
              onSelectProject(e.target.value || null)
            }
            className="rounded bg-gray-800 px-3 py-1 text-sm text-gray-300 border border-gray-700 focus:outline-none focus:border-blue-500"
          >
            <option value="">All Projects</option>
            {projects.map((p) => (
              <option key={p} value={p}>
                {p.split('/').pop()}
              </option>
            ))}
          </select>
        ) : projects.length === 1 ? (
          <span className="text-sm text-gray-400">
            {projects[0].split('/').pop()}
          </span>
        ) : null}
      </div>
      <div className="flex items-center gap-5 text-sm text-gray-400">
        {status && (
          <>
            <div className="flex items-center gap-2">
              <StatusDot building={status.building} />
              <span>{status.building ? 'Building' : 'Idle'}</span>
            </div>
            <span>Uptime: {status.uptime}</span>
            <span className="text-gray-500">v{status.version}</span>
          </>
        )}
      </div>
    </header>
  )
}

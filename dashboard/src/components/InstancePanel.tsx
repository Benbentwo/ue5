import type { InstanceInfo } from '../types'
import { postAPI } from '../hooks/useAPI'

interface InstancePanelProps {
  instances: InstanceInfo[]
  onAction: () => void
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

export default function InstancePanel({
  instances,
  onAction,
}: InstancePanelProps) {
  async function handleStop(pid: number) {
    try {
      await postAPI(`/api/instances/${pid}/stop`, {})
      onAction()
    } catch {
      // Errors are non-critical for the UI
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
    </div>
  )
}

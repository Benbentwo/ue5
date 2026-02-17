import type { AgentInfo } from '../types'

interface AgentPanelProps {
  agents: AgentInfo[]
}

function timeAgo(dateStr: string): string {
  const seconds = Math.floor(
    (Date.now() - new Date(dateStr).getTime()) / 1000,
  )
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

export default function AgentPanel({ agents }: AgentPanelProps) {
  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-500">
        Agents
      </h2>

      {agents.length > 0 ? (
        <ul className="flex flex-col gap-2">
          {agents.map((agent) => (
            <li
              key={agent.id}
              className="rounded-lg bg-gray-900 p-4 flex flex-col gap-1"
            >
              <div className="flex items-center gap-2">
                <span className="inline-flex h-2.5 w-2.5 rounded-full bg-blue-400" />
                <span className="font-medium text-gray-200">{agent.name}</span>
              </div>
              <p className="text-sm text-gray-400">{agent.description}</p>
              <p className="text-xs text-gray-500">
                Last seen {timeAgo(agent.last_seen_at)}
              </p>
            </li>
          ))}
        </ul>
      ) : (
        <div className="rounded-lg bg-gray-900 p-4">
          <p className="text-sm text-gray-500">No registered agents</p>
        </div>
      )}
    </div>
  )
}

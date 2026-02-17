import { useState, useMemo } from 'react'
import type {
  DaemonStatus,
  BuildInfo,
  InstanceInfo,
  AgentInfo,
} from './types'
import { useFetch } from './hooks/useAPI'
import { useSSE } from './hooks/useSSE'
import Header from './components/Header'
import BuildPanel from './components/BuildPanel'
import InstancePanel from './components/InstancePanel'
import AgentPanel from './components/AgentPanel'

function App() {
  const {
    data: status,
    refetch: refetchStatus,
    setData: setStatus,
  } = useFetch<DaemonStatus>('/api/status')

  const {
    data: buildInfo,
    refetch: refetchBuild,
    setData: setBuildInfo,
  } = useFetch<BuildInfo>('/api/build')

  const {
    data: instances,
    refetch: refetchInstances,
    setData: setInstances,
  } = useFetch<InstanceInfo[]>('/api/instances')

  const {
    data: agents,
    refetch: refetchAgents,
    setData: setAgents,
  } = useFetch<AgentInfo[]>('/api/agents')

  const [selectedProject, setSelectedProject] = useState<string | null>(null)

  // Collect unique project paths from instances and build history
  const projects = useMemo(() => {
    const paths = new Set<string>()
    if (instances) {
      for (const inst of instances) {
        paths.add(inst.project_path)
      }
    }
    if (buildInfo?.recent_builds) {
      for (const b of buildInfo.recent_builds) {
        paths.add(b.project_path)
      }
    }
    return Array.from(paths).sort()
  }, [instances, buildInfo])

  // Auto-select when only one project
  useMemo(() => {
    if (projects.length === 1 && selectedProject !== projects[0]) {
      setSelectedProject(projects[0])
    }
  }, [projects, selectedProject])

  // SSE handlers for real-time updates
  useSSE('/api/events', {
    snapshot: (data) => {
      const snap = data as {
        status: DaemonStatus
        build: BuildInfo
        instances: InstanceInfo[]
        agents: AgentInfo[]
      }
      setStatus(snap.status)
      setBuildInfo(snap.build)
      setInstances(snap.instances)
      setAgents(snap.agents)
    },
    editor_state_changed: () => {
      refetchInstances()
      refetchStatus()
    },
    rebuild_started: () => {
      refetchBuild()
      refetchStatus()
    },
    rebuild_complete: () => {
      refetchBuild()
      refetchStatus()
    },
    agent_registered: () => {
      refetchAgents()
      refetchStatus()
    },
    agent_unregistered: () => {
      refetchAgents()
      refetchStatus()
    },
  })

  // Filter instances by selected project
  const filteredInstances = useMemo(() => {
    if (!instances) return []
    if (!selectedProject) return instances
    return instances.filter((i) => i.project_path === selectedProject)
  }, [instances, selectedProject])

  return (
    <div className="flex min-h-screen flex-col bg-gray-950 text-gray-100">
      <Header
        status={status}
        projects={projects}
        selectedProject={selectedProject}
        onSelectProject={setSelectedProject}
      />
      <main className="flex-1 grid grid-cols-1 gap-6 p-6 md:grid-cols-3">
        <BuildPanel
          buildInfo={buildInfo}
          selectedProject={selectedProject}
          onBuildTriggered={refetchBuild}
        />
        <InstancePanel instances={filteredInstances} onAction={refetchInstances} />
        <AgentPanel agents={agents ?? []} />
      </main>
    </div>
  )
}

export default App

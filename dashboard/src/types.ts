export interface BuildRecord {
  id: string
  project_path: string
  labels: string[]
  features: string[]
  contributions: BuildContribution[]
  mode: 'full' | 'hot_reload'
  status: 'pending' | 'building' | 'succeeded' | 'failed'
  started_at: string
  completed_at?: string
  error?: string
  target: string
  platform: string
  configuration: string
}

export interface BuildContribution {
  agent_id: string
  label: string
}

export interface BuildInfo {
  current_build?: BuildRecord
  accumulated_features: string[]
  total_builds: number
  recent_builds: BuildRecord[]
}

export interface InstanceInfo {
  project_path: string
  project_name: string
  engine_path: string
  engine_version: string
  pid: number
  state: 'starting' | 'running' | 'stopping' | 'stopped' | 'crashed'
  started_at: string
  log_file: string
  exit_code?: number
}

export interface AgentInfo {
  id: string
  name: string
  description: string
  registered_at: string
  last_seen_at: string
}

export interface DaemonStatus {
  version: string
  uptime: string
  started_at: string
  instances: number
  agents: number
  building: boolean
}

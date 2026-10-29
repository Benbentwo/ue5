import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

// Build service (pkg/ci) views. Every box serves the same API, so this page
// reads its own box plus every peer in its status and shows them together.

type CIState =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'superseded'
  | 'canceled'
  | 'interrupted'

interface CIJob {
  id: string
  game: string
  kind: 'compile' | 'package' | 'steam'
  platform: string
  repo: string
  sha: string
  branch?: string
  source?: string
  source_url?: string
  state: CIState
  queued_at: string
  started_at?: string
  finished_at?: string
  exit_code?: number
  message?: string
  errors?: string[]
  superseded_by?: string
  retry_of?: string
  box: string
  position?: number
  duration_sec: number
  url: string
}

interface CIStatus {
  box: string
  enabled: boolean
  public_url: string
  running?: CIJob
  queued: number
  free_gb: number
  disk_floor_gb: number
  work_root: string
  peers: string[]
  reporting: boolean
  report_error?: string
  now: string
}

interface BoxData {
  base: string // '' for this box
  status: CIStatus | null
  jobs: CIJob[]
  error?: string
}

const POLL_MS = 5000

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return res.json()
}

function useBoxes(): BoxData[] {
  const [boxes, setBoxes] = useState<BoxData[]>([])
  useEffect(() => {
    let alive = true
    async function load() {
      const one = async (base: string): Promise<BoxData> => {
        try {
          const [status, jobs] = await Promise.all([
            getJSON<CIStatus>(`${base}/api/ci/status`),
            getJSON<{ jobs: CIJob[] }>(`${base}/api/ci/jobs?limit=60`),
          ])
          return { base, status, jobs: jobs.jobs }
        } catch (e) {
          return { base, status: null, jobs: [], error: e instanceof Error ? e.message : String(e) }
        }
      }
      const self = await one('')
      const peers = await Promise.all(
        (self.status?.peers ?? []).map((p) => one(p.replace(/\/$/, ''))),
      )
      if (alive) setBoxes([self, ...peers])
    }
    load()
    const t = setInterval(load, POLL_MS)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [])
  return boxes
}

const stateStyle: Record<CIState, string> = {
  queued: 'bg-gray-700 text-gray-200',
  running: 'bg-yellow-500/20 text-yellow-300',
  succeeded: 'bg-green-500/20 text-green-300',
  failed: 'bg-red-500/20 text-red-300',
  superseded: 'bg-gray-800 text-gray-500',
  canceled: 'bg-gray-800 text-gray-400',
  interrupted: 'bg-orange-500/20 text-orange-300',
}

function StateBadge({ state }: { state: CIState }) {
  return (
    <span className={`rounded px-2 py-0.5 text-xs font-medium ${stateStyle[state]}`}>
      {state}
    </span>
  )
}

function dur(sec: number): string {
  if (!sec) return ''
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  return h ? `${h}h ${m}m` : m ? `${m}m ${s}s` : `${s}s`
}

function ago(iso?: string): string {
  if (!iso) return ''
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

function note(j: CIJob): string {
  if (j.state === 'queued' && j.position) return `#${j.position} in line${j.message ? ' · ' + j.message : ''}`
  if (j.errors?.length) return j.errors[0]
  return j.message ?? ''
}

function BoxCard({ box }: { box: BoxData }) {
  const s = box.status
  if (!s) {
    return (
      <div className="rounded-lg border border-red-900 bg-gray-900 p-4">
        <div className="font-semibold text-gray-200">{box.base || 'this box'}</div>
        <div className="mt-1 text-sm text-red-400">unreachable: {box.error}</div>
      </div>
    )
  }
  const low = s.free_gb < s.disk_floor_gb
  return (
    <div className="rounded-lg border border-gray-800 bg-gray-900 p-4">
      <div className="flex items-center justify-between">
        <div className="font-semibold text-gray-100">{s.box}</div>
        <span className={`text-xs ${s.enabled ? 'text-green-400' : 'text-gray-500'}`}>
          {s.enabled ? 'taking builds' : 'build service off'}
        </span>
      </div>
      <div className="mt-2 text-sm text-gray-300">
        {s.running ? (
          <a href={`#/ci/${s.running.id}`} className="hover:underline">
            <span className="text-yellow-300">building</span> {s.running.game} {s.running.kind}{' '}
            {s.running.platform} · {dur(s.running.duration_sec)}
          </a>
        ) : (
          <span className="text-gray-500">idle</span>
        )}
      </div>
      <div className="mt-2 flex gap-4 text-xs text-gray-400">
        <span>{s.queued} queued</span>
        <span className={low ? 'text-red-400' : ''}>
          {s.free_gb} GB free{low ? ` (floor ${s.disk_floor_gb})` : ''}
        </span>
        <span className={s.reporting ? '' : 'text-orange-400'}>
          {s.reporting ? 'reports to Azure' : 'no Azure PAT: not reporting'}
        </span>
      </div>
      {s.report_error && (
        <div className="mt-2 text-xs text-red-400">
          Azure reporting is failing: {s.report_error}. Run <code>ue5 ci doctor</code> on {s.box}.
        </div>
      )}
    </div>
  )
}

function JobTable({ jobs }: { jobs: CIJob[] }) {
  const [game, setGame] = useState('')
  const [hideSuperseded, setHideSuperseded] = useState(true)
  const games = useMemo(() => Array.from(new Set(jobs.map((j) => j.game))).sort(), [jobs])
  const shown = jobs.filter(
    (j) => (!game || j.game === game) && !(hideSuperseded && j.state === 'superseded'),
  )
  return (
    <div className="rounded-lg border border-gray-800 bg-gray-900">
      <div className="flex items-center gap-4 border-b border-gray-800 px-4 py-2 text-sm">
        <select
          value={game}
          onChange={(e) => setGame(e.target.value)}
          className="rounded border border-gray-700 bg-gray-800 px-2 py-1 text-gray-300"
        >
          <option value="">All games</option>
          {games.map((g) => (
            <option key={g}>{g}</option>
          ))}
        </select>
        <label className="flex items-center gap-2 text-gray-400">
          <input type="checkbox" checked={hideSuperseded} onChange={(e) => setHideSuperseded(e.target.checked)} />
          hide superseded
        </label>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="text-xs uppercase text-gray-500">
            <tr>
              <th className="px-4 py-2">State</th>
              <th className="px-4 py-2">Game</th>
              <th className="px-4 py-2">Job</th>
              <th className="px-4 py-2">Commit</th>
              <th className="px-4 py-2">Box</th>
              <th className="px-4 py-2">When</th>
              <th className="px-4 py-2">Took</th>
              <th className="px-4 py-2">Note</th>
            </tr>
          </thead>
          <tbody>
            {shown.map((j) => (
              <tr
                key={j.id}
                onClick={() => (window.location.hash = `#/ci/${j.id}`)}
                className={`cursor-pointer border-t border-gray-800 hover:bg-gray-800/60 ${j.state === 'superseded' ? 'opacity-50' : ''}`}
              >
                <td className="px-4 py-2"><StateBadge state={j.state} /></td>
                <td className="px-4 py-2 text-gray-200">{j.game}</td>
                <td className="px-4 py-2 text-gray-300">{j.kind} · {j.platform}</td>
                <td className="px-4 py-2 font-mono text-xs text-gray-400">{j.sha.slice(0, 9)}</td>
                <td className="px-4 py-2 text-gray-400">{j.box}</td>
                <td className="px-4 py-2 text-gray-400">{ago(j.queued_at)}</td>
                <td className="px-4 py-2 text-gray-400">{dur(j.duration_sec)}</td>
                <td className="max-w-md truncate px-4 py-2 text-gray-400" title={note(j)}>{note(j)}</td>
              </tr>
            ))}
            {shown.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-6 text-center text-gray-500">No builds yet.</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

const TOKEN_KEY = 'ue5-ci-token'

function readToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}

function JobDetail({ id, boxes }: { id: string; boxes: BoxData[] }) {
  const found = boxes.find((b) => b.jobs.some((j) => j.id === id))
  const base = found?.base ?? ''
  const [job, setJob] = useState<CIJob | null>(null)
  const [log, setLog] = useState('')
  const [actionError, setActionError] = useState<string | null>(null)
  const size = useRef(0)
  const pre = useRef<HTMLPreElement>(null)

  useEffect(() => {
    let alive = true
    async function load() {
      try {
        const j = await getJSON<CIJob>(`${base}/api/ci/jobs/${id}`)
        if (!alive) return
        setJob(j)
        const q = size.current === 0 ? 'tail=200000' : `offset=${size.current}`
        const res = await fetch(`${base}/api/ci/jobs/${id}/log?${q}`)
        const text = await res.text()
        const n = Number(res.headers.get('X-Log-Size') ?? '0')
        if (!alive) return
        if (text) {
          const atBottom = pre.current
            ? pre.current.scrollHeight - pre.current.scrollTop - pre.current.clientHeight < 40
            : true
          setLog((old) => (size.current === 0 ? text : old + text))
          if (atBottom) requestAnimationFrame(() => pre.current?.scrollTo(0, pre.current.scrollHeight))
        }
        size.current = n
      } catch {
        /* the next poll retries */
      }
    }
    load()
    const t = setInterval(load, 2000)
    return () => {
      alive = false
      clearInterval(t)
    }
  }, [id, base])

  const act = useCallback(
    async (action: 'cancel' | 'retry') => {
      setActionError(null)
      let token = readToken()
      if (!token) {
        token = window.prompt(`Paste the dispatch token from ~/.ue5/ci/token on ${job?.box ?? 'the build box'}`) ?? ''
        if (!token) return
        try {
          localStorage.setItem(TOKEN_KEY, token.trim())
        } catch {
          /* private window: ask again next time */
        }
      }
      const res = await fetch(`${base}/api/ci/jobs/${id}/${action}`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${token.trim()}` },
      })
      const body = await res.json().catch(() => ({}))
      if (!res.ok) {
        if (res.status === 401) {
          try {
            localStorage.removeItem(TOKEN_KEY)
          } catch {
            /* ignore */
          }
        }
        setActionError(body.error ?? res.statusText)
        return
      }
      if (action === 'retry' && body.id) window.location.hash = `#/ci/${body.id}`
    },
    [base, id, job?.box],
  )

  if (!job) return <div className="text-gray-500">Loading build {id}…</div>
  const live = job.state === 'running' || job.state === 'queued'
  return (
    <div className="flex flex-col gap-4">
      <a href="#/ci" className="text-sm text-blue-400 hover:underline">← all builds</a>
      <div className="rounded-lg border border-gray-800 bg-gray-900 p-4">
        <div className="flex flex-wrap items-center gap-3">
          <StateBadge state={job.state} />
          <span className="text-lg font-semibold text-gray-100">
            {job.game} {job.kind} · {job.platform}
          </span>
          <span className="font-mono text-sm text-gray-400">{job.sha.slice(0, 12)}</span>
          <span className="text-sm text-gray-400">on {job.box}</span>
          <span className="ml-auto flex gap-2">
            {live && (
              <button onClick={() => act('cancel')} className="rounded bg-gray-800 px-3 py-1 text-sm text-gray-200 hover:bg-gray-700">
                Cancel
              </button>
            )}
            {!live && (
              <button onClick={() => act('retry')} className="rounded bg-blue-600 px-3 py-1 text-sm text-white hover:bg-blue-500">
                Retry
              </button>
            )}
          </span>
        </div>
        {actionError && <div className="mt-2 text-sm text-red-400">{actionError}</div>}
        <dl className="mt-3 grid grid-cols-2 gap-x-6 gap-y-1 text-sm md:grid-cols-4">
          <dt className="text-gray-500">Queued</dt><dd className="text-gray-300">{ago(job.queued_at)}</dd>
          <dt className="text-gray-500">Took</dt><dd className="text-gray-300">{dur(job.duration_sec) || '—'}</dd>
          <dt className="text-gray-500">From</dt>
          <dd className="text-gray-300">
            {job.source_url ? <a className="text-blue-400 hover:underline" href={job.source_url}>{job.source}</a> : job.source || '—'}
          </dd>
          <dt className="text-gray-500">Exit</dt><dd className="text-gray-300">{job.exit_code ?? '—'}</dd>
          {job.message && (<><dt className="text-gray-500">Note</dt><dd className="col-span-3 text-gray-300">{job.message}</dd></>)}
        </dl>
        {job.errors && job.errors.length > 0 && (
          <div className="mt-4">
            <div className="mb-1 text-xs uppercase text-gray-500">Key error lines</div>
            <ul className="space-y-1 font-mono text-xs text-red-300">
              {job.errors.map((e, i) => (
                <li key={i} className="break-all">{e}</li>
              ))}
            </ul>
          </div>
        )}
      </div>
      <pre ref={pre} className="h-[60vh] overflow-auto rounded-lg border border-gray-800 bg-black p-3 font-mono text-xs leading-5 text-gray-300">
        {log || (job.state === 'queued' ? 'Waiting to start…' : 'No log.')}
      </pre>
    </div>
  )
}

export default function Builds({ jobId }: { jobId?: string }) {
  const boxes = useBoxes()
  const jobs = useMemo(
    () => boxes.flatMap((b) => b.jobs).sort((a, b) => b.queued_at.localeCompare(a.queued_at)),
    [boxes],
  )
  if (jobId) return <JobDetail key={jobId} id={jobId} boxes={boxes} />
  return (
    <div className="flex flex-col gap-6">
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
        {boxes.map((b) => (
          <BoxCard key={b.base || 'self'} box={b} />
        ))}
      </div>
      <JobTable jobs={jobs} />
    </div>
  )
}

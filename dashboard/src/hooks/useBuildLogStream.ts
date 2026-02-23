import { useState, useEffect, useRef, useCallback } from 'react'
import type { LogLineEvent } from '../types'

const API_BASE = import.meta.env.DEV ? 'http://localhost:9516' : ''

export function useBuildLogStream(projectPath: string | null, active: boolean) {
  const [lines, setLines] = useState<LogLineEvent[]>([])
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)

  const disconnect = useCallback(() => {
    if (esRef.current) {
      esRef.current.close()
      esRef.current = null
    }
    setConnected(false)
  }, [])

  useEffect(() => {
    if (!active || !projectPath) {
      disconnect()
      return
    }

    setLines([])

    const url = `${API_BASE}/api/build/logs/stream?project=${encodeURIComponent(projectPath)}`
    const es = new EventSource(url)
    esRef.current = es

    es.addEventListener('build_log_history', (e) => {
      const data = JSON.parse(e.data) as { lines: LogLineEvent[] }
      setLines(data.lines)
    })

    es.addEventListener('build_log', (e) => {
      const line = JSON.parse(e.data) as LogLineEvent
      setLines((prev) => [...prev, line])
    })

    es.addEventListener('build_log_end', () => {
      disconnect()
    })

    es.onopen = () => setConnected(true)

    es.onerror = () => {
      disconnect()
    }

    return () => disconnect()
  }, [active, projectPath, disconnect])

  return { lines, connected }
}

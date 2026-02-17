import { useEffect, useRef, useCallback } from 'react'

type EventHandler = (data: unknown) => void

export function useSSE(url: string, handlers: Record<string, EventHandler>) {
  const handlersRef = useRef(handlers)
  handlersRef.current = handlers

  const connect = useCallback(() => {
    const es = new EventSource(url)
    let retryDelay = 1000

    es.onerror = () => {
      es.close()
      setTimeout(() => connect(), retryDelay)
      retryDelay = Math.min(retryDelay * 2, 30000)
    }

    const eventTypes = [
      'snapshot',
      'editor_state_changed',
      'rebuild_started',
      'rebuild_complete',
      'agent_registered',
      'agent_unregistered',
    ]

    for (const type of eventTypes) {
      es.addEventListener(type, (e) => {
        const data = JSON.parse(e.data)
        if (handlersRef.current[type]) {
          handlersRef.current[type](data)
        }
      })
    }

    return es
  }, [url])

  useEffect(() => {
    const es = connect()
    return () => es.close()
  }, [connect])
}

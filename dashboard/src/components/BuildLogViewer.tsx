import { useEffect, useRef } from 'react'
import type { LogLineEvent } from '../types'

interface BuildLogViewerProps {
  lines: LogLineEvent[]
}

function levelColor(level: string): string {
  switch (level) {
    case 'error':
      return 'text-red-400'
    case 'warning':
      return 'text-yellow-400'
    default:
      return 'text-gray-300'
  }
}

export default function BuildLogViewer({ lines }: BuildLogViewerProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  function handleScroll() {
    const el = containerRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 32
    autoScrollRef.current = atBottom
  }

  useEffect(() => {
    const el = containerRef.current
    if (el && autoScrollRef.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [lines])

  return (
    <div className="mt-2 flex flex-col gap-1">
      <div className="flex items-center justify-between">
        <span className="text-xs text-gray-500">Build Output</span>
        <span className="text-xs text-gray-600">{lines.length} lines</span>
      </div>
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="max-h-72 overflow-y-auto rounded bg-gray-950 border border-gray-800 p-2 font-mono text-xs leading-relaxed"
      >
        {lines.map((line, i) => (
          <div key={i} className={levelColor(line.level)}>
            {line.raw}
          </div>
        ))}
        {lines.length === 0 && (
          <div className="text-gray-600">Waiting for build output...</div>
        )}
      </div>
    </div>
  )
}

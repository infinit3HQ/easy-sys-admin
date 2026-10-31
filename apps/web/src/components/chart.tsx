import { useMemo, useState } from 'react'

export interface Series {
  key: string
  label: string
  color: string
  values: Array<number>
}

/**
 * A small SVG line chart drawn like a chart recorder trace. Values are
 * percentages unless max is given. Hover shows the values at that time.
 */
export function LineChart({ times, series, max = 100, height = 180, unit = '%' }: { times: Array<string>; series: Array<Series>; max?: number; height?: number; unit?: string }) {
  const [hover, setHover] = useState<number | null>(null)
  const width = 800
  const pad = { l: 34, r: 8, t: 10, b: 20 }
  const w = width - pad.l - pad.r
  const h = height - pad.t - pad.b
  const n = times.length

  const paths = useMemo(
    () =>
      series.map((s) => {
        if (n === 0) return ''
        return s.values
          .map((v, i) => {
            const x = pad.l + (n === 1 ? w : (i / (n - 1)) * w)
            const y = pad.t + h - (Math.min(v, max) / max) * h
            return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
          })
          .join('')
      }),
    [series, n, w, h, max, pad.l, pad.t],
  )

  if (n === 0) {
    return <div className="grid h-40 place-items-center font-mono text-xs text-muted">No samples yet. The poller records one per minute.</div>
  }

  const ticks = [0, 25, 50, 75, 100].map((p) => (p / 100) * max)
  const labelAt = (i: number) => new Date(times[i] ?? '').toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  const hx = hover === null ? null : pad.l + (n === 1 ? w : (hover / (n - 1)) * w)

  return (
    <div className="relative">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="w-full"
        onMouseLeave={() => setHover(null)}
        onMouseMove={(e) => {
          const r = e.currentTarget.getBoundingClientRect()
          const x = ((e.clientX - r.left) / r.width) * width - pad.l
          setHover(Math.max(0, Math.min(n - 1, Math.round((x / w) * (n - 1)))))
        }}
      >
        {ticks.map((t) => {
          const y = pad.t + h - (t / max) * h
          return (
            <g key={t}>
              <line x1={pad.l} x2={width - pad.r} y1={y} y2={y} stroke="var(--color-line)" strokeDasharray={t === 0 ? undefined : '2 4'} />
              <text x={pad.l - 6} y={y + 3} textAnchor="end" className="fill-faint font-mono text-[9px]">
                {Math.round(t)}
              </text>
            </g>
          )
        })}
        {series.map((s, i) => (
          <path key={s.key} d={paths[i]} fill="none" stroke={s.color} strokeWidth={1.6} strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
        ))}
        <text x={pad.l} y={height - 4} className="fill-faint font-mono text-[9px]" suppressHydrationWarning>
          {labelAt(0)}
        </text>
        <text x={width - pad.r} y={height - 4} textAnchor="end" className="fill-faint font-mono text-[9px]" suppressHydrationWarning>
          {labelAt(n - 1)}
        </text>
        {hx !== null && <line x1={hx} x2={hx} y1={pad.t} y2={pad.t + h} stroke="var(--color-amber)" strokeWidth={1} opacity={0.6} />}
      </svg>
      <div className="mt-2 flex flex-wrap items-center gap-4 font-mono text-[11px]">
        {series.map((s) => (
          <span key={s.key} className="inline-flex items-center gap-1.5 text-ink-2">
            <span className="h-0.5 w-3" style={{ background: s.color }} />
            {s.label}
            {hover !== null && <span className="text-ink">{(s.values[hover] ?? 0).toFixed(1)}{unit}</span>}
          </span>
        ))}
        {hover !== null && <span className="ml-auto text-muted">{labelAt(hover)}</span>}
      </div>
    </div>
  )
}

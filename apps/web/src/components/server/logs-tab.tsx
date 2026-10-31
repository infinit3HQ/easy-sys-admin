import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Button, ErrorBanner, Field, Loading, Notice, cx } from '#/components/ui'
import { useRole } from '#/routes/app/route'

const priorities = [
  [-1, 'all'],
  [3, 'errors'],
  [4, 'warnings'],
  [6, 'info'],
] as const

export function LogsTab({ serverId }: { serverId: number }) {
  const { canAct } = useRole()
  const containers = useQuery(q.containers(serverId))
  const [source, setSource] = useState<{ kind: 'unit' | 'container'; name: string }>({ kind: 'unit', name: 'ssh.service' })
  const [lines, setLines] = useState(200)
  const [priority, setPriority] = useState(-1)
  const [grep, setGrep] = useState('')
  const [submitted, setSubmitted] = useState({ ...source, lines, priority, grep })

  useEffect(() => {
    // Links from the services tab put the unit name in the URL hash.
    const h = decodeURIComponent(window.location.hash.slice(1))
    if (h.endsWith('.service')) {
      setSource({ kind: 'unit', name: h })
      setSubmitted((s) => ({ ...s, kind: 'unit', name: h }))
    }
  }, [])

  const logs = useQuery({
    queryKey: ['logs', serverId, submitted],
    enabled: canAct && !!submitted.name,
    queryFn: () =>
      call(
        api().GET('/servers/{serverId}/logs', {
          params: {
            path: { serverId },
            query: {
              unit: submitted.kind === 'unit' ? submitted.name : undefined,
              container: submitted.kind === 'container' ? submitted.name : undefined,
              lines: submitted.lines,
              priority: submitted.kind === 'unit' ? submitted.priority : undefined,
              grep: submitted.grep || undefined,
            },
          },
        }),
      ),
  })

  if (!canAct) return <Notice>Logs can contain secrets, so reading them needs the admin role.</Notice>

  return (
    <div className="space-y-4">
      <form
        className="panel grid gap-4 p-4 md:grid-cols-[1fr_2fr_100px_120px_1fr_auto] md:items-end"
        onSubmit={(e) => {
          e.preventDefault()
          setSubmitted({ ...source, lines, priority, grep })
        }}
      >
        <Field label="Source">
          <select className="input" value={source.kind} onChange={(e) => setSource({ kind: e.target.value as 'unit' | 'container', name: '' })}>
            <option value="unit">systemd unit</option>
            <option value="container">container</option>
          </select>
        </Field>
        <Field label={source.kind === 'unit' ? 'Unit' : 'Container'}>
          {source.kind === 'container' ? (
            <select className="input font-mono" value={source.name} onChange={(e) => setSource({ ...source, name: e.target.value })}>
              <option value="">pick one</option>
              {(containers.data?.items ?? []).map((c) => (
                <option key={c.id} value={c.name}>
                  {c.name}
                </option>
              ))}
            </select>
          ) : (
            <input className="input font-mono" value={source.name} onChange={(e) => setSource({ ...source, name: e.target.value })} placeholder="nginx.service" />
          )}
        </Field>
        <Field label="Lines">
          <input className="input font-mono" type="number" min={1} max={1000} value={lines} onChange={(e) => setLines(Number(e.target.value))} />
        </Field>
        <Field label="Priority">
          <select className="input" value={priority} disabled={source.kind === 'container'} onChange={(e) => setPriority(Number(e.target.value))}>
            {priorities.map(([v, l]) => (
              <option key={v} value={v}>
                {l}
              </option>
            ))}
          </select>
        </Field>
        <Field label="Contains">
          <input className="input font-mono" value={grep} onChange={(e) => setGrep(e.target.value)} />
        </Field>
        <Button type="submit" variant="primary" busy={logs.isFetching}>
          Read
        </Button>
      </form>
      <ErrorBanner error={logs.error} />
      {logs.isLoading ? (
        <Loading label="Reading logs" />
      ) : (
        <div className="panel max-h-[65vh] overflow-auto bg-bg-2 p-3 font-mono text-[11px] leading-relaxed">
          {(logs.data?.items ?? []).length === 0 && <p className="text-muted">No lines.</p>}
          {(logs.data?.items ?? []).map((l, i) => (
            <div key={i} className="flex gap-3 whitespace-pre-wrap break-all">
              <span className="shrink-0 text-faint">{new Date(l.time).toLocaleTimeString()}</span>
              <span className={cx(l.priority <= 3 || l.stream === 'stderr' ? 'text-fail' : l.priority === 4 ? 'text-warn' : 'text-ink-2')}>{l.message}</span>
            </div>
          ))}
        </div>
      )}
      <p className="font-mono text-[10px] text-faint">Reading logs is recorded in the activity log.</p>
    </div>
  )
}

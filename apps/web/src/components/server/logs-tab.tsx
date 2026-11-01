import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { ScrollTextIcon } from 'lucide-react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import {
  Choice,
  ErrorBanner,
  Field,
  Loading,
  Notice,
  NumberInput,
  cn,
} from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import { Input } from '#/components/ui/input'
import { useServerAccess } from '#/components/server/use-access'

const priorities = [
  { value: '-1', label: 'all' },
  { value: '3', label: 'errors' },
  { value: '4', label: 'warnings' },
  { value: '6', label: 'info' },
]

export function LogsTab({ serverId }: { serverId: number }) {
  const { data: detail } = useSuspenseQuery(q.server(serverId))
  const { canOperate: canAct, ownerTeam } = useServerAccess(detail.server)
  const containers = useQuery(q.containers(serverId))
  const [source, setSource] = useState<{
    kind: 'unit' | 'container'
    name: string
  }>({ kind: 'unit', name: 'ssh.service' })
  const [lines, setLines] = useState(200)
  const [priority, setPriority] = useState(-1)
  const [grep, setGrep] = useState('')
  const [submitted, setSubmitted] = useState({
    ...source,
    lines,
    priority,
    grep,
  })

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
              container:
                submitted.kind === 'container' ? submitted.name : undefined,
              lines: submitted.lines,
              priority:
                submitted.kind === 'unit' ? submitted.priority : undefined,
              grep: submitted.grep || undefined,
            },
          },
        }),
      ),
  })

  if (detail.server.access === 'view')
    return (
      <Notice>
        Logs need operate access. {ownerTeam} shares this server with view
        access only.
      </Notice>
    )
  if (!canAct)
    return (
      <Notice>
        Logs can contain secrets, so reading them needs admin rights on this
        server. Ask an admin of the organization to add you to a team that holds
        its group.
      </Notice>
    )

  const items = logs.data?.items ?? []

  return (
    <div className="space-y-4">
      <Card>
        <form
          className="grid grid-cols-2 gap-4 p-4 md:grid-cols-[1fr_2fr_100px_130px_1fr_auto] md:items-end"
          onSubmit={(e) => {
            e.preventDefault()
            setSubmitted({ ...source, lines, priority, grep })
          }}
        >
          <Field label="Source" className="col-span-2 sm:col-span-1">
            <Choice
              value={source.kind}
              onChange={(v) =>
                setSource({ kind: v as 'unit' | 'container', name: '' })
              }
              options={[
                { value: 'unit', label: 'systemd unit' },
                { value: 'container', label: 'container' },
              ]}
            />
          </Field>
          <Field
            label={source.kind === 'unit' ? 'Unit' : 'Container'}
            className="col-span-2 sm:col-span-1"
          >
            {source.kind === 'container' ? (
              <Choice
                className="font-mono"
                placeholder="pick one"
                value={source.name}
                onChange={(v) => setSource({ ...source, name: v })}
                options={(containers.data?.items ?? []).map((c) => ({
                  value: c.name,
                  label: c.name,
                }))}
              />
            ) : (
              <Input
                className="font-mono"
                autoCapitalize="off"
                spellCheck={false}
                value={source.name}
                onChange={(e) => setSource({ ...source, name: e.target.value })}
                placeholder="nginx.service"
              />
            )}
          </Field>
          <Field label="Lines">
            <NumberInput min={1} max={1000} value={lines} onChange={setLines} />
          </Field>
          <Field label="Priority">
            <Choice
              disabled={source.kind === 'container'}
              value={String(priority)}
              onChange={(v) => setPriority(Number(v))}
              options={priorities}
            />
          </Field>
          <Field label="Contains" className="col-span-2 md:col-span-1">
            <Input
              className="font-mono"
              autoCapitalize="off"
              spellCheck={false}
              value={grep}
              onChange={(e) => setGrep(e.target.value)}
            />
          </Field>
          <Button
            type="submit"
            busy={logs.isFetching}
            className="col-span-2 md:col-span-1"
          >
            <ScrollTextIcon /> Read
          </Button>
        </form>
      </Card>
      <ErrorBanner error={logs.error} />
      {logs.isLoading ? (
        <Loading label="Reading logs" />
      ) : (
        <div className="max-h-[65dvh] overflow-auto rounded-md border bg-bg-2 p-3 font-mono text-[11px] leading-relaxed">
          {items.length === 0 && (
            <p className="text-muted-foreground">No lines.</p>
          )}
          {items.map((l, i) => (
            <div
              key={i}
              className="flex flex-col gap-x-3 border-b border-line/40 py-1 last:border-0 sm:flex-row sm:border-0 sm:py-0"
            >
              <span className="shrink-0 text-faint" suppressHydrationWarning>
                {new Date(l.time).toLocaleTimeString()}
              </span>
              <span
                className={cn(
                  'break-all whitespace-pre-wrap',
                  l.priority <= 3 || l.stream === 'stderr'
                    ? 'text-fail'
                    : l.priority === 4
                      ? 'text-warn'
                      : 'text-ink-2',
                )}
              >
                {l.message}
              </span>
            </div>
          ))}
        </div>
      )}
      <p className="font-mono text-[10px] text-faint">
        Reading logs is recorded in the activity log.
      </p>
    </div>
  )
}

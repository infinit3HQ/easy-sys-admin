import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { SearchIcon } from 'lucide-react'
import type { ServiceUnit } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { bytes } from '#/lib/format'
import { Ago } from '#/components/time'
import { ConfirmDialog, Empty, ErrorBanner, Loading, Segmented, cn } from '#/components/kit'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import { Input } from '#/components/ui/input'
import { useRole } from '#/routes/app/route'

type Action = 'start' | 'stop' | 'restart'
type Show = 'active' | 'failed' | 'all'

export function ServicesTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const list = useQuery(q.services(serverId))
  const [filter, setFilter] = useState('')
  const [show, setShow] = useState<Show>('active')
  const [pending, setPending] = useState<{
    unit: ServiceUnit
    action: Action
  } | null>(null)

  const act = useMutation({
    mutationFn: ({ unit, action }: { unit: string; action: Action }) =>
      call(
        api().POST('/servers/{serverId}/services/{unit}/{action}', {
          params: { path: { serverId, unit, action } },
        }),
      ),
    onSuccess: async () => {
      setPending(null)
      await new Promise((r) => setTimeout(r, 1500))
      await qc.invalidateQueries({ queryKey: ['services', serverId] })
    },
  })

  const items = useMemo(() => {
    const all = list.data?.items ?? []
    return all.filter(
      (s) =>
        (show === 'all' || (show === 'failed' ? s.active === 'failed' : s.active !== 'inactive')) &&
        (!filter || s.unit.includes(filter) || s.description.toLowerCase().includes(filter.toLowerCase())),
    )
  }, [list.data, filter, show])

  if (list.isLoading) return <Loading />
  if (list.error) return <ErrorBanner error={list.error} />

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
        <div className="relative sm:w-72">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
          <Input className="pl-9 font-mono" placeholder="Filter units" aria-label="Filter units" value={filter} onChange={(e) => setFilter(e.target.value)} />
        </div>
        <div className="flex items-center justify-between gap-3 sm:contents">
          <Segmented
            size="sm"
            aria-label="Which units"
            value={show}
            onChange={setShow}
            options={[
              { value: 'active', label: 'active' },
              { value: 'failed', label: 'failed' },
              { value: 'all', label: 'all' },
            ]}
          />
          <span className="font-mono text-xs text-muted-foreground sm:ml-auto">{items.length} units</span>
        </div>
      </div>

      {items.length === 0 ? (
        <Empty title="No units match" />
      ) : (
        <Card>
          <ul className="divide-y divide-line/60">
            {items.map((s) => (
              <li key={s.unit} className="grid gap-x-4 gap-y-2 px-4 py-3 md:grid-cols-[minmax(0,1fr)_150px_80px_80px_90px_auto] md:items-center md:py-2.5">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span
                      className={cn(
                        'font-mono text-xs break-all',
                        s.active === 'failed' ? 'text-fail' : s.active === 'active' ? 'text-ink' : 'text-muted-foreground',
                      )}
                    >
                      {s.unit}
                    </span>
                    {s.protected && <Badge variant="amber">protected</Badge>}
                  </div>
                  <div className="truncate text-xs text-muted-foreground">{s.description}</div>
                </div>
                <div className="flex flex-wrap gap-x-3 gap-y-1 font-mono text-[11px] md:contents">
                  <span>
                    <span className={s.active === 'failed' ? 'text-fail' : s.active === 'active' ? 'text-ok' : 'text-muted-foreground'}>{s.active}</span>
                    <span className="text-faint"> / {s.sub}</span>
                  </span>
                  <span className="text-muted-foreground">{s.enabled}</span>
                  <span className="num text-ink-2">{s.memoryBytes ? bytes(s.memoryBytes) : ''}</span>
                  <span className="text-muted-foreground">{s.since ? <Ago iso={s.since} /> : ''}</span>
                </div>
                <div className="flex flex-wrap gap-1 md:justify-end">
                  <Button asChild size="xs" variant="ghost">
                    <Link to="." search={{ tab: 'logs' }} hash={s.unit}>
                      logs
                    </Link>
                  </Button>
                  {canAct &&
                    (s.active === 'active' ? (['restart', 'stop'] as const) : (['start'] as const)).map((a) => (
                      <Button key={a} size="xs" variant="outline" disabled={a === 'stop' && s.protected} onClick={() => setPending({ unit: s, action: a })}>
                        {a}
                      </Button>
                    ))}
                </div>
              </li>
            ))}
          </ul>
        </Card>
      )}
      <ConfirmDialog
        open={!!pending}
        title={`${pending?.action ?? ''} ${pending?.unit.unit ?? ''}`}
        danger={pending?.action !== 'start'}
        confirmWord={pending?.unit.protected ? pending.unit.unit : undefined}
        body={
          pending?.unit.protected ? 'This unit is protected. Restarting it can drop connections to the server.' : 'The action is written to the activity log.'
        }
        actionLabel={pending?.action ?? ''}
        busy={act.isPending}
        error={act.error}
        onConfirm={() => pending && act.mutate({ unit: pending.unit.unit, action: pending.action })}
        onClose={() => setPending(null)}
      />
    </div>
  )
}

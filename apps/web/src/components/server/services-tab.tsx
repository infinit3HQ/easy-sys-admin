import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import type { ServiceUnit } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { bytes } from '#/lib/format'
import { Ago } from '#/components/time'
import { Button, ConfirmDialog, ErrorBanner, Loading, cx } from '#/components/ui'
import { useRole } from '#/routes/app/route'

type Action = 'start' | 'stop' | 'restart'

export function ServicesTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const list = useQuery(q.services(serverId))
  const [filter, setFilter] = useState('')
  const [show, setShow] = useState<'active' | 'all' | 'failed'>('active')
  const [pending, setPending] = useState<{ unit: ServiceUnit; action: Action } | null>(null)

  const act = useMutation({
    mutationFn: ({ unit, action }: { unit: string; action: Action }) =>
      call(api().POST('/servers/{serverId}/services/{unit}/{action}', { params: { path: { serverId, unit, action } } })),
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
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <input className="input max-w-xs font-mono" placeholder="filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
        {(['active', 'failed', 'all'] as const).map((s) => (
          <button key={s} onClick={() => setShow(s)} className={cx('font-mono text-xs uppercase', show === s ? 'text-amber' : 'text-muted hover:text-ink')}>
            {s}
          </button>
        ))}
        <span className="ml-auto font-mono text-xs text-muted">{items.length} units</span>
      </div>
      <div className="panel overflow-x-auto">
        <table className="w-full min-w-[760px] text-left text-sm">
          <tbody>
            {items.map((s) => (
              <tr key={s.unit} className="border-b border-line/60 last:border-0">
                <td className="px-3 py-2">
                  <span className={cx('font-mono text-xs', s.active === 'failed' ? 'text-fail' : s.active === 'active' ? 'text-ink' : 'text-muted')}>
                    {s.unit}
                  </span>
                  {s.protected && <span className="ml-2 font-mono text-[9px] uppercase text-amber">protected</span>}
                  <div className="truncate text-xs text-muted">{s.description}</div>
                </td>
                <td className="px-3 py-2 font-mono text-[11px]">
                  <span className={s.active === 'failed' ? 'text-fail' : s.active === 'active' ? 'text-ok' : 'text-muted'}>{s.active}</span>
                  <span className="text-faint"> / {s.sub}</span>
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-muted">{s.enabled}</td>
                <td className="num px-3 py-2 text-[11px] text-ink-2">{s.memoryBytes ? bytes(s.memoryBytes) : ''}</td>
                <td className="px-3 py-2 font-mono text-[11px] text-muted">{s.since ? <Ago iso={s.since} /> : ''}</td>
                <td className="px-3 py-2 text-right">
                  <div className="flex justify-end gap-1">
                    <Link to="." search={{ tab: 'logs' }} hash={s.unit} className="rounded px-2 py-1 font-mono text-[10px] uppercase text-muted hover:text-ink">
                      logs
                    </Link>
                    {canAct &&
                      (s.active === 'active' ? (['restart', 'stop'] as const) : (['start'] as const)).map((a) => (
                        <Button key={a} size="sm" variant="ghost" disabled={a === 'stop' && s.protected} onClick={() => setPending({ unit: s, action: a })}>
                          {a}
                        </Button>
                      ))}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ConfirmDialog
        open={!!pending}
        title={`${pending?.action ?? ''} ${pending?.unit.unit ?? ''}`}
        danger={pending?.action !== 'start'}
        confirmWord={pending?.unit.protected ? pending.unit.unit : undefined}
        body={pending?.unit.protected ? 'This unit is protected. Restarting it can drop connections to the server.' : 'The action is written to the activity log.'}
        actionLabel={pending?.action ?? ''}
        busy={act.isPending}
        error={act.error}
        onConfirm={() => pending && act.mutate({ unit: pending.unit.unit, action: pending.action })}
        onClose={() => setPending(null)}
      />
    </div>
  )
}

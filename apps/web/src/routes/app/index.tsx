import { Link, createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import type { Server } from '@esa/api'
import { q } from '#/lib/queries'
import { duration, pct } from '#/lib/format'
import { Ago } from '#/components/time'
import { Empty, Meter, PageHeader, Stat, StatusDot, cx } from '#/components/ui'
import { useRole } from './route'

export const Route = createFileRoute('/app/')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.overview()),
  head: () => ({ meta: [{ title: 'Board · easy-sys-admin' }] }),
  component: Board,
})

function Board() {
  const { data } = useSuspenseQuery(q.overview())
  const { canAct } = useRole()
  const t = data.totals

  return (
    <div>
      <PageHeader kicker="Fleet" title="The board">
        {canAct && (
          <Link to="/app/servers/new" className="rounded bg-amber px-4 py-2 text-sm font-semibold text-bg hover:bg-amber-2">
            Add server
          </Link>
        )}
      </PageHeader>

      <div className="panel mb-6 grid grid-cols-2 gap-6 p-5 sm:grid-cols-3 lg:grid-cols-6 animate-rise" style={{ animationDelay: '60ms' }}>
        <Stat label="Servers" value={t.servers} sub={`${t.groups} groups`} />
        <Stat label="OK" value={t.ok} tone={t.ok ? 'ok' : undefined} />
        <Stat label="Warning" value={t.warning} tone={t.warning ? 'warn' : undefined} />
        <Stat label="Failing" value={t.failing} tone={t.failing ? 'fail' : undefined} />
        <Stat label="Unreachable" value={t.unreachable} tone={t.unreachable ? 'down' : undefined} />
        <Stat label="Pending" value={t.pending} />
      </div>

      {data.servers.length === 0 ? (
        <Empty title="No servers yet">
          Add a server with a one-time sudo login, or get a command to paste on it. The control plane installs a small helper and starts
          watching it within a minute.
        </Empty>
      ) : (
        <div className="panel overflow-x-auto animate-rise" style={{ animationDelay: '120ms' }}>
          <table className="w-full min-w-[960px] border-collapse text-left">
            <thead>
              <tr className="border-b border-line">
                {['', 'Server', 'CPU', 'Memory', 'Disk', 'Load', 'Up', 'Updates', 'Why', 'Seen'].map((h) => (
                  <th key={h} className="label px-3 py-2.5 font-normal">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {data.servers.map((s) => (
                <Row key={s.id} s={s} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

function Row({ s }: { s: Server }) {
  const m = s.metrics
  const reasons = s.status === 'unreachable' ? [s.lastError || 'no answer'] : s.reasons
  return (
    <tr className={cx('group border-b border-line/60 transition-colors last:border-0 hover:bg-panel-2', s.status === 'failing' && 'bg-fail/[0.04]')}>
      <td className="w-6 py-3 pl-4 pr-1">
        <StatusDot status={s.status} />
      </td>
      <td className="px-3 py-3">
        <Link to="/app/servers/$serverId" params={{ serverId: s.id }} className="block">
          <span className="font-mono text-sm text-ink group-hover:text-amber">{s.name}</span>
          <span className="mt-0.5 block font-mono text-[10px] uppercase tracking-wider text-faint">
            {s.groupName ?? 'no group'} · {s.host}
          </span>
        </Link>
      </td>
      <Gauge value={m?.cpuPercent} warn={85} fail={97} />
      <Gauge value={m?.memPercent} warn={90} fail={97} />
      <Gauge value={m?.diskPercent} warn={85} fail={95} />
      <td className="num px-3 py-3 text-xs text-ink-2">{m ? m.load1.toFixed(2) : '—'}</td>
      <td className="num px-3 py-3 text-xs text-ink-2">{m ? duration(m.uptimeSeconds) : '—'}</td>
      <td className="num px-3 py-3 text-xs">
        {s.updates ? (
          <span>
            <span className={s.updates.security ? 'text-warn' : 'text-ink-2'}>{s.updates.security} sec</span>
            <span className="text-faint"> · </span>
            <span className="text-ink-2">{s.updates.other}</span>
          </span>
        ) : (
          <span className="text-faint">—</span>
        )}
      </td>
      <td className="max-w-[260px] px-3 py-3">
        <span className={cx('line-clamp-2 text-xs', s.status === 'ok' ? 'text-faint' : 'text-ink-2')}>
          {s.hostKeyMismatch ? 'host key changed: paused' : reasons.length ? reasons.join(' · ') : s.enrollment === 'pending' ? 'waiting for enroll command' : 'all clear'}
        </span>
      </td>
      <td className="num whitespace-nowrap px-3 py-3 pr-4 text-[11px] text-muted"><Ago iso={s.lastSeenAt} /></td>
    </tr>
  )
}

function Gauge({ value, warn, fail }: { value: number | undefined; warn: number; fail: number }) {
  return (
    <td className="w-28 px-3 py-3">
      <div className="num mb-1 text-xs text-ink-2">{pct(value)}</div>
      <Meter value={value} warn={warn} fail={fail} />
    </td>
  )
}

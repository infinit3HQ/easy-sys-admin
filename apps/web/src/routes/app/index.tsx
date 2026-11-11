import { Link, createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { PlusIcon } from 'lucide-react'
import type { Server } from '@esa/api'
import { q } from '#/lib/queries'
import { duration, pct, plural } from '#/lib/format'
import { Ago } from '#/components/time'
import { Empty, Meter, PageHeader, Stat, StatusDot, cn } from '#/components/kit'
import { clickableRowClass, followRowLink } from '#/components/row-link'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '#/components/ui/table'
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
          <Button asChild>
            <Link to="/app/servers/new">
              <PlusIcon /> Add server
            </Link>
          </Button>
        )}
      </PageHeader>

      <Card
        className="mb-6 grid animate-rise grid-cols-3 gap-x-4 gap-y-5 p-4 sm:p-5 lg:grid-cols-6"
        style={{ animationDelay: '60ms' }}
      >
        <Stat
          label="Servers"
          value={t.servers}
          sub={plural(t.groups, 'group')}
        />
        <Stat label="OK" value={t.ok} tone={t.ok ? 'ok' : undefined} />
        <Stat
          label="Warning"
          value={t.warning}
          tone={t.warning ? 'warn' : undefined}
        />
        <Stat
          label="Failing"
          value={t.failing}
          tone={t.failing ? 'fail' : undefined}
        />
        <Stat
          label="Unreachable"
          value={t.unreachable}
          tone={t.unreachable ? 'down' : undefined}
        />
        <Stat label="Pending" value={t.pending} />
      </Card>

      {data.servers.length === 0 ? (
        <Empty
          title="No servers yet"
          action={
            canAct && (
              <Button asChild>
                <Link to="/app/servers/new">
                  <PlusIcon /> Add server
                </Link>
              </Button>
            )
          }
        >
          Add a server with a one-time sudo login, or get a command to paste on
          it. The control plane installs a small helper and starts watching it
          within a minute.
        </Empty>
      ) : (
        <div className="animate-rise" style={{ animationDelay: '120ms' }}>
          {/* Phones and tablets: one card per server. */}
          <ul className="grid gap-3 md:grid-cols-2 xl:hidden">
            {data.servers.map((s) => (
              <li key={s.id}>
                <ServerCard s={s} />
              </li>
            ))}
          </ul>
          {/* Wide screens: the full table. */}
          <Card className="hidden xl:flex">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-6 pr-1 pl-4">
                    <span className="sr-only">Status</span>
                  </TableHead>
                  {[
                    'Server',
                    'CPU',
                    'Memory',
                    'Disk',
                    'Load',
                    'Up',
                    'Updates',
                    'Exposure',
                    'Why',
                    'Seen',
                  ].map((h) => (
                    <TableHead key={h}>{h}</TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.servers.map((s) => (
                  <Row key={s.id} s={s} />
                ))}
              </TableBody>
            </Table>
          </Card>
        </div>
      )}
    </div>
  )
}

/** Names the organization that shares a server with this one. */
function OwnerTag({ s }: { s: Server }) {
  if (s.access === 'own' || !s.ownerTeam) return null
  return (
    <span
      className="ml-1.5 rounded-sm border border-line-2 px-1 text-[9px] tracking-normal text-muted-foreground normal-case"
      title={`Shared by ${s.ownerTeam}`}
    >
      {s.ownerTeam}
    </span>
  )
}

function why(s: Server): string {
  const reasons =
    s.status === 'unreachable' ? [s.lastError || 'no answer'] : s.reasons
  if (s.hostKeyMismatch) return 'host key changed: paused'
  if (reasons.length) return reasons.join(' · ')
  if (s.enrollment === 'pending') return 'waiting for enroll command'
  return 'all clear'
}

function ExposureCell({ s, className }: { s: Server; className?: string }) {
  const e = s.exposure
  if (!e) return <span className={cn('text-xs text-faint', className)}>—</span>
  if (!e.ready) {
    return (
      <span
        className={cn('text-xs text-warn', className)}
        title="This helper is too old to collect the exposure summary. Reinstall it."
      >
        reinstall helper
      </span>
    )
  }
  const bits: string[] = []
  if (e.publicPorts.length)
    bits.push(`${e.publicPorts.length} on all interfaces`)
  if (e.passwordAuth) bits.push('password login')
  if (e.permitRootLogin === 'yes') bits.push('root login')
  if (e.permitEmptyPasswords) bits.push('empty passwords')
  if (e.sshRead && !e.pubkeyAuth) bits.push('pubkey off')
  if (e.firewall === 'none') bits.push('no firewall')
  if (e.uncovered > 0) bits.push(`${e.uncovered} not in ufw`)
  if (e.extraRoot > 0) bits.push('extra uid 0')
  if (e.noPasswordAll > 0) bits.push('NOPASSWD ALL')
  const warn = bits.some((b) => !b.endsWith('on all interfaces'))
  return (
    <span
      className={cn(
        'line-clamp-2 text-xs',
        warn ? 'text-warn' : 'text-faint',
        className,
      )}
      title={e.publicPorts.length ? e.publicPorts.join(', ') : undefined}
    >
      {bits.length ? bits.join(' · ') : 'none'}
    </span>
  )
}

function Updates({ s }: { s: Server }) {
  if (!s.updates) return <span className="text-faint">—</span>
  const { security, other } = s.updates
  if (!security && !other) return <span className="text-faint">none</span>
  return (
    <span title={`${security} security, ${other} other`}>
      <span className="text-ink-2">{security + other}</span>
      {security > 0 && (
        <span className="text-warn"> · {security} security</span>
      )}
    </span>
  )
}

function Row({ s }: { s: Server }) {
  const m = s.metrics
  return (
    <TableRow
      className={cn(
        'group',
        clickableRowClass,
        s.status === 'failing' && 'bg-fail/[0.04]',
      )}
      onClick={followRowLink}
      onAuxClick={followRowLink}
    >
      <TableCell className="w-6 py-3 pr-1 pl-4">
        <StatusDot status={s.status} />
      </TableCell>
      <TableCell className="py-3">
        <Link
          to="/app/servers/$serverId"
          params={{ serverId: s.id }}
          className="block"
        >
          <span className="font-mono text-sm text-ink group-hover:text-amber">
            {s.name}
          </span>
          <span className="mt-0.5 block font-mono text-[10px] tracking-wider text-faint uppercase">
            {s.groupName ?? 'no group'}
            <OwnerTag s={s} /> · {s.host}
          </span>
        </Link>
      </TableCell>
      <Gauge value={m?.cpuPercent} warn={85} fail={97} />
      <Gauge value={m?.memPercent} warn={90} fail={97} />
      <Gauge value={m?.diskPercent} warn={85} fail={95} />
      <TableCell className="num py-3 text-xs text-ink-2">
        {m ? m.load1.toFixed(2) : '—'}
      </TableCell>
      <TableCell className="num py-3 text-xs text-ink-2">
        {m ? duration(m.uptimeSeconds) : '—'}
      </TableCell>
      <TableCell className="num py-3 text-xs">
        <Updates s={s} />
      </TableCell>
      <TableCell className="max-w-[220px] py-3">
        <ExposureCell s={s} />
      </TableCell>
      <TableCell className="max-w-[260px] py-3">
        <span
          className={cn(
            'line-clamp-2 text-xs',
            s.status === 'ok' ? 'text-faint' : 'text-ink-2',
          )}
        >
          {why(s)}
        </span>
      </TableCell>
      <TableCell className="num py-3 pr-4 text-[11px] whitespace-nowrap text-muted-foreground">
        <Ago iso={s.lastSeenAt} />
      </TableCell>
    </TableRow>
  )
}

function Gauge({
  value,
  warn,
  fail,
}: {
  value: number | undefined
  warn: number
  fail: number
}) {
  return (
    <TableCell className="w-28 py-3">
      <div className="num mb-1 text-xs text-ink-2">{pct(value)}</div>
      <Meter value={value} warn={warn} fail={fail} />
    </TableCell>
  )
}

function ServerCard({ s }: { s: Server }) {
  const m = s.metrics
  return (
    <Link
      to="/app/servers/$serverId"
      params={{ serverId: s.id }}
      className={cn(
        'panel group block p-4 transition-colors hover:border-amber/50 active:bg-panel-2',
        s.status === 'failing' && 'border-fail/40',
        s.status === 'warning' && 'border-warn/30',
      )}
    >
      <div className="flex items-start gap-3">
        <StatusDot status={s.status} className="mt-1.5" />
        <div className="min-w-0 flex-1">
          <div className="truncate font-mono text-sm text-ink group-hover:text-amber">
            {s.name}
          </div>
          <div className="truncate font-mono text-[10px] tracking-wider text-faint uppercase">
            {s.groupName ?? 'no group'}
            <OwnerTag s={s} /> · {s.host}
          </div>
        </div>
        <span className="num shrink-0 text-[10px] text-muted-foreground">
          <Ago iso={s.lastSeenAt} />
        </span>
      </div>
      <div className="mt-4 grid grid-cols-3 gap-3">
        {(
          [
            ['CPU', m?.cpuPercent, 85, 97],
            ['Mem', m?.memPercent, 90, 97],
            ['Disk', m?.diskPercent, 85, 95],
          ] as const
        ).map(([label, v, warn, fail]) => (
          <div key={label}>
            <div className="mb-1 flex items-baseline justify-between gap-1">
              <span className="label">{label}</span>
              <span className="num text-[11px] text-ink-2">{pct(v)}</span>
            </div>
            <Meter value={v} warn={warn} fail={fail} />
          </div>
        ))}
      </div>
      <div className="mt-3 flex items-center justify-between gap-3 border-t border-line/60 pt-3 font-mono text-[11px]">
        <span
          className={cn(
            'min-w-0 truncate',
            s.status === 'ok' ? 'text-faint' : 'text-ink-2',
          )}
        >
          {why(s)}
        </span>
        <span className="num shrink-0">
          <Updates s={s} />
        </span>
      </div>
      <ExposureCell s={s} className="mt-2" />
    </Link>
  )
}

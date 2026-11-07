import { Link, createFileRoute } from '@tanstack/react-router'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import type { Server, ServerStatus } from '@esa/api'
import { pct } from '#/lib/format'
import { q } from '#/lib/queries'
import { Ago, Until } from '#/components/time'
import { RunsTable } from '#/components/runs-table'
import { Meter, Stat, StatusDot, cn } from '#/components/kit'
import { Card } from '#/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '#/components/ui/table'
import { clickableRowClass, followRowLink } from '#/components/row-link'
import { useGroupAccess } from '#/components/server/use-access'
import { Route as GroupRoute } from './route'

export const Route = createFileRoute('/app/groups/$groupId/')({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(q.group(params.groupId)),
  component: GroupOverview,
})

function GroupOverview() {
  const { groupId } = GroupRoute.useParams()
  const { data: g } = useSuspenseQuery(q.group(groupId))
  const overview = useQuery(q.overview())
  const members = (overview.data?.servers ?? []).filter(
    (s) => s.groupId === groupId,
  )
  const { isOwn } = useGroupAccess(g)
  const counts = countStatus(members)

  return (
    <div>
      <Card className="mb-6 grid grid-cols-3 gap-x-4 gap-y-5 p-4 sm:p-5 lg:grid-cols-6">
        <Stat label="Servers" value={g.serverCount} />
        <Stat
          label="OK"
          value={overview.data ? counts.ok : '—'}
          tone={counts.ok ? 'ok' : undefined}
        />
        <Stat
          label="Warning"
          value={overview.data ? counts.warning : '—'}
          tone={counts.warning ? 'warn' : undefined}
        />
        <Stat
          label="Failing"
          value={overview.data ? counts.failing : '—'}
          tone={counts.failing ? 'fail' : undefined}
        />
        <Stat
          label="Unreachable"
          value={overview.data ? counts.unreachable : '—'}
          tone={counts.unreachable ? 'down' : undefined}
        />
        <Stat label="Pending" value={overview.data ? counts.pending : '—'} />
      </Card>

      <div className="mb-6 grid gap-3 sm:grid-cols-2">
        <Card className="p-4 sm:p-5">
          <Stat
            label="Next clean"
            size="md"
            word={!g.clean.enabled}
            value={g.clean.enabled ? <Until iso={g.nextClean} /> : 'Off'}
          />
        </Card>
        <Card className="p-4 sm:p-5">
          <Stat
            label="Next update"
            size="md"
            word={!g.updates.enabled}
            value={g.updates.enabled ? <Until iso={g.nextUpdate} /> : 'Off'}
            sub={
              g.updates.enabled ? `${g.updates.batchSize} at a time` : undefined
            }
          />
        </Card>
      </div>

      <section className="mb-8">
        <h2 className="label mb-3">Servers</h2>
        {members.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {overview.isPending
              ? 'Loading servers…'
              : isOwn
                ? 'No servers yet. Move one here from its settings tab.'
                : 'No servers in it right now.'}
          </p>
        ) : (
          <>
            <ul className="grid gap-3 md:grid-cols-2 xl:hidden">
              {members.map((s) => (
                <li key={s.id}>
                  <ServerCard s={s} />
                </li>
              ))}
            </ul>
            <Card className="hidden xl:flex">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-6 pr-1 pl-4">
                      <span className="sr-only">Status</span>
                    </TableHead>
                    {['Server', 'CPU', 'Memory', 'Disk', 'Seen'].map((h) => (
                      <TableHead key={h}>{h}</TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {members.map((s) => (
                    <ServerRow key={s.id} s={s} />
                  ))}
                </TableBody>
              </Table>
            </Card>
          </>
        )}
      </section>

      <section>
        <h2 className="label mb-3">Recent runs</h2>
        <RunsTable runs={g.lastRuns} />
      </section>
    </div>
  )
}

function countStatus(servers: Array<Server>) {
  const counts: Record<ServerStatus, number> = {
    ok: 0,
    warning: 0,
    failing: 0,
    unreachable: 0,
    pending: 0,
  }
  for (const s of servers) counts[s.status] += 1
  return counts
}

function ServerCard({ s }: { s: Server }) {
  const m = s.metrics
  return (
    <Link
      to="/app/servers/$serverId"
      params={{ serverId: s.id }}
      className={cn(
        'panel group block p-4 transition-colors hover:border-amber/50',
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
            {s.host}
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
    </Link>
  )
}

function ServerRow({ s }: { s: Server }) {
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
            {s.host}
          </span>
        </Link>
      </TableCell>
      <Gauge value={m?.cpuPercent} warn={85} fail={97} />
      <Gauge value={m?.memPercent} warn={90} fail={97} />
      <Gauge value={m?.diskPercent} warn={85} fail={95} />
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

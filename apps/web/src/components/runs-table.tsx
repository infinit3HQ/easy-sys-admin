import { Link } from '@tanstack/react-router'
import type { Run } from '@esa/api'
import { bytes } from '#/lib/format'
import { Ago } from '#/components/time'
import { Empty, RunStatus, cn } from '#/components/kit'
import { clickableRowClass, followRowLink } from '#/components/row-link'
import { Card } from '#/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '#/components/ui/table'

function target(r: Run) {
  return r.groupName ?? r.servers.map((s) => s.serverName).join(', ')
}

function Result({ r }: { r: Run }) {
  return (
    <>
      {r.summary || '…'}
      {r.kind === 'clean' && r.bytesFreed > 0 && (
        <span className="ml-2 text-ok">{bytes(r.bytesFreed)}</span>
      )}
    </>
  )
}

/**
 * Runs as a table when there is room and as a list of cards when there is
 * not. It measures its container, not the screen.
 */
export function RunsTable({
  runs,
  empty = 'No runs yet',
}: {
  runs: Array<Run>
  empty?: string
}) {
  if (runs.length === 0) return <Empty title={empty} />
  return (
    <div className="@container">
      <Card className="@3xl:hidden">
        <ul className="divide-y divide-line/60">
          {runs.map((r) => (
            <li key={r.id}>
              <Link
                to="/app/runs/$runId"
                params={{ runId: r.id }}
                className="block px-4 py-3 transition-colors active:bg-panel-2"
              >
                <div className="flex items-center justify-between gap-3">
                  <span className="flex min-w-0 items-baseline gap-2">
                    <span className="font-mono text-xs text-amber">
                      #{r.id}
                    </span>
                    <span className="font-mono text-xs text-ink uppercase">
                      {r.kind}
                    </span>
                    <span className="font-mono text-[10px] text-faint">
                      {r.trigger}
                    </span>
                  </span>
                  <RunStatus status={r.status} />
                </div>
                <div className="mt-1 truncate text-xs text-ink-2">
                  {target(r)}
                </div>
                <div className="mt-1 flex items-baseline justify-between gap-3 font-mono text-[11px] text-muted-foreground">
                  <span className="min-w-0 truncate">
                    <Result r={r} />
                  </span>
                  <span className="shrink-0">
                    <Ago iso={r.startedAt} />
                  </span>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      </Card>
      <Card className="hidden @3xl:flex">
        <Table className="min-w-[720px]">
          <TableHeader>
            <TableRow>
              {['Run', 'Kind', 'Target', 'Status', 'Result', 'Started'].map(
                (h, i) => (
                  <TableHead
                    key={h}
                    className={i === 0 ? 'pl-4' : i === 5 ? 'pr-4' : undefined}
                  >
                    {h}
                  </TableHead>
                ),
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {runs.map((r) => (
              <TableRow
                key={r.id}
                className={cn('group', clickableRowClass)}
                onClick={followRowLink}
                onAuxClick={followRowLink}
              >
                <TableCell className="pl-4">
                  <Link
                    to="/app/runs/$runId"
                    params={{ runId: r.id }}
                    className="font-mono text-xs text-amber group-hover:underline"
                  >
                    #{r.id}
                  </Link>
                </TableCell>
                <TableCell className="font-mono text-xs text-ink uppercase">
                  {r.kind}
                </TableCell>
                <TableCell className="text-xs text-ink-2">
                  {target(r)}
                  <span className="ml-2 font-mono text-[10px] text-faint">
                    {r.trigger}
                  </span>
                </TableCell>
                <TableCell>
                  <RunStatus status={r.status} />
                </TableCell>
                <TableCell className="font-mono text-[11px] text-muted-foreground">
                  <Result r={r} />
                </TableCell>
                <TableCell className="pr-4 font-mono text-[11px] whitespace-nowrap text-muted-foreground">
                  <Ago iso={r.startedAt} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Card>
    </div>
  )
}

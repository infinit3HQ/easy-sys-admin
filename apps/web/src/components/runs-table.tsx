import { Link } from '@tanstack/react-router'
import type { Run } from '@esa/api'
import { bytes } from '#/lib/format'
import { Ago } from '#/components/time'
import { Empty, RunStatus } from '#/components/ui'

export function RunsTable({ runs, empty = 'No runs yet' }: { runs: Array<Run>; empty?: string }) {
  if (runs.length === 0) return <Empty title={empty} />
  return (
    <div className="panel overflow-x-auto">
      <table className="w-full min-w-[720px] text-left text-sm">
        <thead>
          <tr className="border-b border-line">
            {['Run', 'Kind', 'Target', 'Status', 'Result', 'Started'].map((h) => (
              <th key={h} className="label px-3 py-2.5 font-normal">
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {runs.map((r) => (
            <tr key={r.id} className="border-b border-line/60 last:border-0 hover:bg-panel-2">
              <td className="px-3 py-2.5">
                <Link to="/app/runs/$runId" params={{ runId: r.id }} className="font-mono text-xs text-amber hover:underline">
                  #{r.id}
                </Link>
              </td>
              <td className="px-3 py-2.5 font-mono text-xs uppercase text-ink">{r.kind}</td>
              <td className="px-3 py-2.5 text-xs text-ink-2">
                {r.groupName ?? r.servers.map((s) => s.serverName).join(', ')}
                <span className="ml-2 font-mono text-[10px] text-faint">{r.trigger}</span>
              </td>
              <td className="px-3 py-2.5">
                <RunStatus status={r.status} />
              </td>
              <td className="px-3 py-2.5 font-mono text-[11px] text-muted">
                {r.summary || '…'}
                {r.kind === 'clean' && r.bytesFreed > 0 && <span className="ml-2 text-ok">{bytes(r.bytesFreed)}</span>}
              </td>
              <td className="whitespace-nowrap px-3 py-2.5 font-mono text-[11px] text-muted"><Ago iso={r.startedAt} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

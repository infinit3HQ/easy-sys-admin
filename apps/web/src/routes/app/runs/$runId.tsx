import { Link, createFileRoute } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { z } from 'zod'
import type { ReactNode } from 'react'
import type { RunServer } from '@esa/api'
import { q } from '#/lib/queries'
import { bytes } from '#/lib/format'
import { When } from '#/components/time'
import { PageHeader, Panel, RunStatus } from '#/components/ui'

export const Route = createFileRoute('/app/runs/$runId')({
  params: { parse: (p) => ({ runId: z.coerce.number().int().positive().parse(p.runId) }), stringify: (p) => ({ runId: String(p.runId) }) },
  loader: ({ context, params }) => context.queryClient.ensureQueryData(q.run(params.runId)),
  head: ({ params }) => ({ meta: [{ title: `Run #${params.runId} · easy-sys-admin` }] }),
  component: RunPage,
})

function RunPage() {
  const { runId } = Route.useParams()
  const { data: r } = useSuspenseQuery(q.run(runId))
  return (
    <div>
      <PageHeader kicker={<Link to="/app/runs">Runs</Link>} title={`${r.kind} #${r.id}`}>
        <RunStatus status={r.status} />
      </PageHeader>
      <div className="panel mb-6 grid gap-4 p-5 font-mono text-xs sm:grid-cols-4">
        <Fact k="Target" v={r.groupName ?? 'selected servers'} />
        <Fact k="Trigger" v={r.trigger} />
        <Fact k="Started" v={<When iso={r.startedAt} />} />
        <Fact k="Finished" v={r.finishedAt ? <When iso={r.finishedAt} /> : 'running'} />
        <div className="sm:col-span-4">
          <Fact k="Summary" v={r.summary || '…'} />
        </div>
      </div>
      <div className="space-y-4">
        {r.servers.map((s) => (
          <ServerResult key={s.serverId} s={s} kind={r.kind} />
        ))}
      </div>
    </div>
  )
}

function ServerResult({ s, kind }: { s: RunServer; kind: string }) {
  const d = s.detail as Record<string, any>
  return (
    <Panel
      title={
        <Link to="/app/servers/$serverId" params={{ serverId: s.serverId }} className="hover:text-amber">
          {s.serverName}
        </Link>
      }
      actions={<RunStatus status={s.status} />}
    >
      {s.error && <p className="mb-3 rounded border border-fail/40 bg-fail/10 px-3 py-2 font-mono text-xs text-fail">{s.error}</p>}
      {(kind === 'clean' || kind === 'measure') && Array.isArray(d.items) && (
        <table className="w-full text-left font-mono text-xs">
          <tbody>
            {(d.items as Array<Record<string, any>>).map((it) => (
              <tr key={it.id} className="border-t border-line/60 first:border-0">
                <td className="py-1.5 text-ink">{it.id}</td>
                <td className="py-1.5 text-ink-2">{it.status}</td>
                <td className="py-1.5 text-muted">{it.reason ?? it.error ?? ''}</td>
                <td className="py-1.5 text-right">{kind === 'measure' ? `${bytes(it.reclaimable)} reclaimable` : <span className="text-ok">{bytes(it.freed)} freed</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {kind === 'update' && d.upgrade && (
        <div className="space-y-3 font-mono text-xs">
          <p className="text-ink-2">
            {d.upgrade.packages?.length ? `${d.upgrade.packages.length} packages upgraded: ${d.upgrade.packages.join(', ')}` : 'Nothing to upgrade.'}
          </p>
          {d.upgrade.skipped?.length > 0 && <p className="text-muted">Held back: {d.upgrade.skipped.join(', ')}</p>}
          {d.reboot && <p className="text-amber">Reboot: {d.reboot}</p>}
          {d.upgrade.rebootRequired && !d.reboot && <p className="text-warn">The server needs a reboot.</p>}
          {d.upgrade.logTail?.length > 0 && (
            <pre className="max-h-64 overflow-auto rounded border border-line bg-bg-2 p-3 text-[11px] text-ink-2">{d.upgrade.logTail.join('\n')}</pre>
          )}
        </div>
      )}
      {kind === 'update' && d.precheck && <p className="font-mono text-xs text-fail">{(d.precheck.problems ?? []).join('; ')}</p>}
      {kind === 'reboot' && d.reboot && <p className="font-mono text-xs text-ok">{d.reboot}</p>}
      {s.status === 'running' && <p className="font-mono text-xs text-amber">Working…</p>}
    </Panel>
  )
}

function Fact({ k, v }: { k: string; v: ReactNode }) {
  return (
    <div>
      <div className="label">{k}</div>
      <div className="mt-1 text-ink">{v}</div>
    </div>
  )
}

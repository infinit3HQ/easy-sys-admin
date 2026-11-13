import { Link, createFileRoute } from '@tanstack/react-router'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { z } from 'zod'
import type { ReactNode } from 'react'
import type { RunServer } from '@esa/api'
import { api, call, orNotFound } from '#/lib/api'
import { q } from '#/lib/queries'
import { bytes } from '#/lib/format'
import { When } from '#/components/time'
import { PageHeader, Panel, RunStatus } from '#/components/kit'
import { Card } from '#/components/ui/card'
import { RunGate } from '#/components/run-gate'
import { useRole } from '../route'

export const Route = createFileRoute('/app/runs/$runId')({
  params: {
    parse: (p) => ({
      runId: z.coerce.number().int().positive().parse(p.runId),
    }),
    stringify: (p) => ({ runId: String(p.runId) }),
  },
  loader: ({ context, params }) =>
    orNotFound(context.queryClient.ensureQueryData(q.run(params.runId))),
  head: ({ params }) => ({
    meta: [{ title: `Run #${params.runId} · easy-sys-admin` }],
  }),
  component: RunPage,
})

function RunPage() {
  const { runId } = Route.useParams()
  const { data: r } = useSuspenseQuery(q.run(runId))
  const { canAct } = useRole()
  const report = useQuery({
    // The run's status is in the key, so the report is fetched once more
    // when the run finishes instead of keeping the last running copy.
    queryKey: ['patch-report', runId, r.status],
    queryFn: () =>
      call(api().GET('/runs/{runId}/report', { params: { path: { runId } } })),
    enabled: r.kind === 'update',
    refetchInterval: r.status === 'running' ? 3000 : false,
  })
  return (
    <div>
      <PageHeader
        kicker={
          <Link to="/app/runs" className="hover:text-ink">
            Runs
          </Link>
        }
        title={`${r.kind} #${r.id}`}
      >
        <RunStatus status={r.status} />
      </PageHeader>
      <RunGate run={r} canAct={canAct} />
      <Card className="mb-6 grid grid-cols-2 gap-x-4 gap-y-4 p-4 font-mono text-xs sm:p-5 lg:grid-cols-4">
        <Fact k="Target" v={r.groupName ?? 'selected servers'} />
        <Fact k="Trigger" v={r.trigger} />
        <Fact k="Started" v={<When iso={r.startedAt} />} />
        <Fact
          k="Finished"
          v={r.finishedAt ? <When iso={r.finishedAt} /> : 'running'}
        />
        <div className="col-span-2 lg:col-span-4">
          <Fact k="Summary" v={r.summary || '…'} />
        </div>
      </Card>
      {report.data && report.data.servers.length > 0 && (
        <Card className="mb-6 p-4 text-sm">
          <p className="text-xs text-muted-foreground">
            {report.data.observeSeconds > 0
              ? `Canaries were watched for ${report.data.observeSeconds} seconds. `
              : ''}
            A skipped server did not get the upgrade.
          </p>
          <ul className="mt-2 space-y-1 font-mono text-xs">
            {report.data.servers.map((s) => (
              <li key={s.serverId}>
                {s.canary ? 'canary ' : ''}
                {s.name}: {s.status}
                {s.packages > 0 ? `, ${s.packages} packages` : ''}
                {s.health ? `, ${s.health}` : ''}
                {s.error ? `, ${s.error}` : ''}
                {s.versions.length > 0 ? (
                  <ul className="text-ink-2">
                    {s.versions.map((v) => (
                      <li key={v.name}>
                        {v.name} {v.before || '?'} → {v.after || 'unknown'}
                      </li>
                    ))}
                  </ul>
                ) : (
                  s.installed.length > 0 && (
                    <div className="text-ink-2">
                      Installed: {s.installed.join(', ')}
                    </div>
                  )
                )}
                {s.held.length > 0 && (
                  <div className="text-muted-foreground">
                    Held: {s.held.join(', ')}
                  </div>
                )}
                {s.notApproved.length > 0 && (
                  <div className="text-warn">
                    Not in the plan, left alone: {s.notApproved.join(', ')}
                  </div>
                )}
              </li>
            ))}
          </ul>
        </Card>
      )}
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
        <Link
          to="/app/servers/$serverId"
          params={{ serverId: s.serverId }}
          className="hover:text-amber"
        >
          {s.serverName}
        </Link>
      }
      actions={<RunStatus status={s.status} />}
    >
      {s.error && (
        <p className="mb-3 rounded-md border border-fail/40 bg-fail/10 px-3 py-2 font-mono text-xs break-all text-fail">
          {s.error}
        </p>
      )}
      {(kind === 'clean' || kind === 'measure') && Array.isArray(d.items) && (
        <ul className="divide-y divide-line/60 font-mono text-xs">
          {(d.items as Array<Record<string, any>>).map((it) => (
            <li
              key={it.id}
              className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-4 gap-y-0.5 py-2 first:pt-0 last:pb-0 sm:grid-cols-[minmax(0,1fr)_100px_minmax(0,1.5fr)_auto]"
            >
              <span className="break-all text-ink">{it.id}</span>
              <span className="text-right sm:order-last">
                {kind === 'measure' ? (
                  `${bytes(it.reclaimable)} reclaimable`
                ) : (
                  <span className="text-ok">{bytes(it.freed)} freed</span>
                )}
              </span>
              <span className="text-ink-2">{it.status}</span>
              <span className="col-span-2 text-muted-foreground sm:col-span-1">
                {it.reason ?? it.error ?? ''}
              </span>
            </li>
          ))}
        </ul>
      )}
      {kind === 'update' && d.upgrade && (
        <div className="space-y-3 font-mono text-xs">
          <p className="break-words text-ink-2">
            {d.upgrade.packages?.length
              ? `${d.upgrade.packages.length} packages upgraded: ${d.upgrade.packages.join(', ')}`
              : 'Nothing to upgrade.'}
          </p>
          {d.upgrade.skipped?.length > 0 && (
            <p className="text-muted-foreground">
              Held back: {d.upgrade.skipped.join(', ')}
            </p>
          )}
          {Array.isArray(d.checks) && d.checks.length > 0 && (
            <ul className="space-y-1 text-ink-2">
              {d.checks.map(
                (c: {
                  name: string
                  phase: string
                  state: string
                  detail?: string
                }) => (
                  <li key={`${c.phase}-${c.name}`}>
                    {c.phase} {c.name}: {c.state}
                    {c.detail ? ` (${c.detail})` : ''}
                  </li>
                ),
              )}
            </ul>
          )}
          {d.reboot && <p className="text-amber">Reboot: {d.reboot}</p>}
          {d.upgrade.rebootRequired && !d.reboot && (
            <p className="text-warn">The server needs a reboot.</p>
          )}
          {d.upgrade.logTail?.length > 0 && (
            <pre className="max-h-64 overflow-auto rounded-md border bg-bg-2 p-3 text-[11px] text-ink-2">
              {d.upgrade.logTail.join('\n')}
            </pre>
          )}
        </div>
      )}
      {kind === 'update' && d.precheck && (
        <p className="font-mono text-xs text-fail">
          {(d.precheck.problems ?? []).join('; ')}
        </p>
      )}
      {kind === 'reboot' && d.reboot && (
        <p className="font-mono text-xs text-ok">{d.reboot}</p>
      )}
      {s.status === 'running' && (
        <p className="font-mono text-xs text-amber">Working…</p>
      )}
    </Panel>
  )
}

function Fact({ k, v }: { k: string; v: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="label">{k}</div>
      <div className="mt-1 break-words text-ink">{v}</div>
    </div>
  )
}

import { Link } from '@tanstack/react-router'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { Button, ConfirmDialog, Empty, ErrorBanner, Notice, Panel, Toggle } from '#/components/ui'
import { useRole } from '#/routes/app/route'

export function UpdatesTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const { data } = useSuspenseQuery(q.server(serverId))
  const upd = data.pendingUpdates
  const [mode, setMode] = useState<'security' | 'all'>('security')
  const [reboot, setReboot] = useState(false)
  const [confirm, setConfirm] = useState<'update' | 'reboot' | null>(null)

  const check = useMutation({
    mutationFn: () => call(api().POST('/servers/{serverId}/updates/check', { params: { path: { serverId } } })),
    onSuccess: (d) => qc.setQueryData(q.server(serverId).queryKey, d),
  })
  const run = useMutation({
    mutationFn: (kind: 'update' | 'reboot') =>
      call(
        api().POST('/runs', {
          body: { kind, serverIds: [serverId], options: kind === 'update' ? { updateMode: mode, rebootIfRequired: reboot, journalSize: '200M' } : undefined },
        }),
      ),
    onSuccess: async () => {
      setConfirm(null)
      await qc.invalidateQueries({ queryKey: ['runs'] })
    },
  })

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <p className="font-mono text-xs text-muted">
          Package lists from <Ago iso={upd?.listsUpdatedAt} /> · checked <Ago iso={data.server.updates?.checkedAt} />
        </p>
        {canAct && (
          <Button onClick={() => check.mutate()} busy={check.isPending}>
            Refresh package lists
          </Button>
        )}
      </div>
      <ErrorBanner error={check.error} />

      {canAct && (
        <Panel title="Install">
          <div className="flex flex-wrap items-end gap-6">
            <div className="flex gap-2">
              {(['security', 'all'] as const).map((m) => (
                <button
                  key={m}
                  onClick={() => setMode(m)}
                  className={`rounded border px-3 py-2 text-sm ${mode === m ? 'border-amber text-amber' : 'border-line-2 text-ink-2'}`}
                >
                  {m === 'security' ? `Security only (${upd?.security ?? 0})` : `Everything (${(upd?.security ?? 0) + (upd?.other ?? 0)})`}
                </button>
              ))}
            </div>
            <Toggle
              checked={reboot}
              onChange={setReboot}
              label="Reboot afterwards if the server asks for it"
              hint={data.server.hostsControlPlane ? 'Skipped: this server hosts esa.' : 'esa waits for it to come back and checks containers.'}
            />
          </div>
          <div className="mt-4 flex flex-wrap gap-3">
            <Button variant="primary" onClick={() => setConfirm('update')}>
              Install updates
            </Button>
            <Button variant="outline" onClick={() => setConfirm('reboot')}>
              Reboot now
            </Button>
          </div>
          <p className="mt-3 text-xs text-muted">
            The upgrade runs as its own systemd job on the server, so it finishes even if the connection drops. Config files you changed are kept.
          </p>
        </Panel>
      )}

      {run.isSuccess && (
        <Notice tone="ok">
          Started. Follow it on the{' '}
          <Link to="/app/runs/$runId" params={{ runId: run.data.id }} className="underline">
            run page
          </Link>
          .
        </Notice>
      )}

      {!upd ? (
        <Empty title="Not checked yet">The poller lists pending updates every six hours.</Empty>
      ) : upd.packages.length === 0 ? (
        <Empty title="Up to date">No pending package updates.</Empty>
      ) : (
        <Panel title={`${upd.packages.length} pending · ${upd.security} security`}>
          <table className="w-full text-left font-mono text-xs">
            <thead>
              <tr className="text-muted">
                <th className="pb-2 font-normal">Package</th>
                <th className="pb-2 font-normal">Installed</th>
                <th className="pb-2 font-normal">Candidate</th>
                <th className="pb-2 font-normal">From</th>
              </tr>
            </thead>
            <tbody>
              {[...upd.packages]
                .sort((a, b) => Number(b.security) - Number(a.security) || a.name.localeCompare(b.name))
                .map((p) => (
                  <tr key={p.name} className="border-t border-line/60">
                    <td className="py-1.5 text-ink">
                      {p.security && <span className="mr-2 rounded-sm bg-warn px-1 text-[9px] font-bold uppercase text-bg">sec</span>}
                      {p.name}
                      {upd.held.includes(p.name) && <span className="ml-2 text-muted">(held)</span>}
                    </td>
                    <td className="py-1.5 text-ink-2">{p.from}</td>
                    <td className="py-1.5 text-ink">{p.to}</td>
                    <td className="py-1.5 text-muted">{p.archive}</td>
                  </tr>
                ))}
            </tbody>
          </table>
        </Panel>
      )}

      <ConfirmDialog
        open={confirm === 'update'}
        title="Install updates"
        body={`Installs ${mode === 'security' ? 'security updates' : 'all pending updates'} on ${data.server.name}${reboot ? ', then reboots if required' : ''}.`}
        actionLabel="Start"
        busy={run.isPending}
        error={run.error}
        onConfirm={() => run.mutate('update')}
        onClose={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={confirm === 'reboot'}
        title="Reboot"
        danger
        confirmWord={data.server.name}
        body={
          data.server.hostsControlPlane
            ? 'This server runs esa itself. The board goes away until it is back up.'
            : 'esa reboots the server, waits up to 10 minutes for it to answer, then checks that containers with a restart policy are running.'
        }
        actionLabel="Reboot"
        busy={run.isPending}
        error={run.error}
        onConfirm={() => run.mutate('reboot')}
        onClose={() => setConfirm(null)}
      />
    </div>
  )
}

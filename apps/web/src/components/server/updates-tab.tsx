import { Link } from '@tanstack/react-router'
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query'
import { useState } from 'react'
import { DownloadIcon, PowerIcon, RefreshCwIcon } from 'lucide-react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import {
  ConfirmDialog,
  Empty,
  ErrorBanner,
  Notice,
  Panel,
  Segmented,
  Toggle,
} from '#/components/kit'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '#/components/ui/table'
import { useServerAccess } from '#/components/server/use-access'

export function UpdatesTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const { data } = useSuspenseQuery(q.server(serverId))
  // Checks and runs need the admin role, and operate access on a shared server.
  const { canOperate: canAct } = useServerAccess(data.server)
  const upd = data.pendingUpdates
  const [mode, setMode] = useState<'security' | 'all'>('security')
  const [reboot, setReboot] = useState(false)
  const [confirm, setConfirm] = useState<'update' | 'reboot' | null>(null)

  const check = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/{serverId}/updates/check', {
          params: { path: { serverId } },
        }),
      ),
    onSuccess: (d) => qc.setQueryData(q.server(serverId).queryKey, d),
  })
  const run = useMutation({
    mutationFn: (kind: 'update' | 'reboot') =>
      call(
        api().POST('/runs', {
          body: {
            kind,
            serverIds: [serverId],
            options:
              kind === 'update'
                ? {
                    updateMode: mode,
                    rebootIfRequired: reboot,
                    journalSize: '200M',
                  }
                : undefined,
          },
        }),
      ),
    onSuccess: async () => {
      setConfirm(null)
      await qc.invalidateQueries({ queryKey: ['runs'] })
    },
  })

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="font-mono text-xs text-muted-foreground">
          Package lists from <Ago iso={upd?.listsUpdatedAt} /> · checked{' '}
          <Ago iso={data.server.updates?.checkedAt} />
        </p>
        {canAct && (
          <Button
            variant="outline"
            onClick={() => check.mutate()}
            busy={check.isPending}
          >
            <RefreshCwIcon /> Refresh package lists
          </Button>
        )}
      </div>
      <ErrorBanner error={check.error} />

      {canAct && (
        <Panel title="Install">
          <div className="flex flex-col gap-4 lg:flex-row lg:flex-wrap lg:items-start lg:gap-8">
            <Segmented
              aria-label="Which updates"
              value={mode}
              onChange={setMode}
              options={[
                {
                  value: 'security',
                  label: `Security only (${upd?.security ?? 0})`,
                },
                {
                  value: 'all',
                  label: `Everything (${(upd?.security ?? 0) + (upd?.other ?? 0)})`,
                },
              ]}
            />
            <Toggle
              checked={reboot}
              onChange={setReboot}
              label="Reboot afterwards if the server asks for it"
              hint={
                data.server.hostsControlPlane
                  ? 'Skipped: this server hosts esa.'
                  : 'esa waits for it to come back and checks containers.'
              }
            />
          </div>
          <div className="mt-5 flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:gap-3">
            <Button onClick={() => setConfirm('update')}>
              <DownloadIcon /> Install updates
            </Button>
            <Button variant="outline" onClick={() => setConfirm('reboot')}>
              <PowerIcon /> Reboot now
            </Button>
          </div>
          <p className="mt-3 text-xs text-muted-foreground">
            The upgrade runs as its own systemd job on the server, so it
            finishes even if the connection drops. Config files you changed are
            kept.
          </p>
        </Panel>
      )}

      {run.isSuccess && (
        <Notice tone="ok">
          Started. Follow it on the{' '}
          <Link
            to="/app/runs/$runId"
            params={{ runId: run.data.id }}
            className="underline underline-offset-4"
          >
            run page
          </Link>
          .
        </Notice>
      )}

      {!upd ? (
        <Empty title="Not checked yet">
          The poller lists pending updates every six hours.
        </Empty>
      ) : upd.packages.length === 0 ? (
        <Empty title="Up to date">No pending package updates.</Empty>
      ) : (
        <Panel
          title={`${upd.packages.length} pending · ${upd.security} security`}
          flush
        >
          <Table className="min-w-[560px] font-mono text-xs">
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Package</TableHead>
                <TableHead>Installed</TableHead>
                <TableHead>Candidate</TableHead>
                <TableHead className="pr-4">From</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...upd.packages]
                .sort(
                  (a, b) =>
                    Number(b.security) - Number(a.security) ||
                    a.name.localeCompare(b.name),
                )
                .map((p) => (
                  <TableRow key={p.name}>
                    <TableCell className="py-2 pl-4 text-ink">
                      <span className="inline-flex flex-wrap items-center gap-2">
                        {p.security && <Badge variant="warn">sec</Badge>}
                        {p.name}
                        {upd.held.includes(p.name) && (
                          <span className="text-muted-foreground">(held)</span>
                        )}
                      </span>
                    </TableCell>
                    <TableCell className="py-2 text-ink-2">{p.from}</TableCell>
                    <TableCell className="py-2 text-ink">{p.to}</TableCell>
                    <TableCell className="py-2 pr-4 text-muted-foreground">
                      {p.archive}
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
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

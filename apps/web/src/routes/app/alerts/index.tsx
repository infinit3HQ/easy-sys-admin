import { Link, createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { SettingsIcon } from 'lucide-react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { Empty, PageHeader, Severity, Toggle, cn } from '#/components/kit'
import { clickableRowClass, followRowLink } from '#/components/row-link'
import { Button } from '#/components/ui/button'
import { useRole } from '../route'
import { Card } from '#/components/ui/card'

function NeedsReview() {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const incidents = useQuery(q.incidents(false))
  const ack = useMutation({
    mutationFn: (id: number) =>
      call(
        api().POST('/incidents/{incidentId}/acknowledge', {
          params: { path: { incidentId: id } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['incidents'] }),
  })
  const snooze = useMutation({
    mutationFn: (id: number) =>
      call(
        api().POST('/incidents/{incidentId}/snooze', {
          params: { path: { incidentId: id } },
          body: {
            until: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString(),
            reason: 'look again tomorrow',
          },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['incidents'] }),
  })
  const items = incidents.data?.items ?? []
  if (items.length === 0) return null
  return (
    <Card className="mb-6">
      <div className="label px-4 pt-4">Needs review</div>
      <ul className="divide-y divide-line">
        {items.map((item) => (
          <li
            key={item.id}
            className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center"
          >
            <div className="min-w-0 flex-1">
              <div className="text-sm break-words text-ink">{item.message}</div>
              <div className="font-mono text-[10px] tracking-wider text-faint uppercase">
                {item.kind} · {item.serverName} · <Ago iso={item.lastSeen} />
              </div>
            </div>
            {canAct && (
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => snooze.mutate(item.id)}
                  busy={snooze.isPending}
                >
                  Tomorrow
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => ack.mutate(item.id)}
                  busy={ack.isPending}
                >
                  Reviewed
                </Button>
              </div>
            )}
          </li>
        ))}
      </ul>
      <p className="px-4 pb-4 text-xs text-muted-foreground">
        Reviewed means you have seen it. Tomorrow hides it for one day, then it
        comes back. Neither changes the server.
      </p>
    </Card>
  )
}

export const Route = createFileRoute('/app/alerts/')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.alerts(false)),
  head: () => ({ meta: [{ title: 'Alerts · easy-sys-admin' }] }),
  component: Alerts,
})

function Alerts() {
  const [all, setAll] = useState(false)
  const alerts = useQuery(q.alerts(all))
  const items = alerts.data?.items ?? []
  return (
    <div>
      <NeedsReview />
      <PageHeader kicker="Signals" title="Alerts">
        <div className="flex flex-wrap items-center gap-3">
          <Toggle checked={all} onChange={setAll} label="Include resolved" />
          <Button asChild variant="outline" size="sm">
            <Link to="/app/alerts/settings">
              <SettingsIcon /> Settings
            </Link>
          </Button>
        </div>
      </PageHeader>
      {items.length === 0 ? (
        <Empty title={all ? 'No alerts recorded' : 'Nothing is wrong'}>
          Rules, channels, and certificates are on the{' '}
          <Link
            to="/app/alerts/settings"
            className="text-amber hover:underline"
          >
            settings page
          </Link>
          .
        </Empty>
      ) : (
        <Card>
          <ul className="divide-y divide-line">
            {items.map((a) => (
              <li
                key={a.id}
                className={cn(
                  'flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:gap-4',
                  a.resolvedAt && 'opacity-50',
                  a.serverId && clickableRowClass,
                )}
                onClick={a.serverId ? followRowLink : undefined}
                onAuxClick={a.serverId ? followRowLink : undefined}
              >
                <div className="flex min-w-0 flex-1 items-start gap-3">
                  <span className="mt-0.5 shrink-0">
                    <Severity severity={a.severity} />
                  </span>
                  <div className="min-w-0">
                    <div className="text-sm break-words text-ink">
                      {a.message}
                    </div>
                    <div className="font-mono text-[10px] tracking-wider text-faint uppercase">
                      {a.kind}
                      {a.serverId && (
                        <>
                          {' · '}
                          <Link
                            to="/app/servers/$serverId"
                            params={{ serverId: a.serverId }}
                            className="hover:text-amber"
                          >
                            {a.serverName}
                          </Link>
                        </>
                      )}
                    </div>
                  </div>
                </div>
                <div className="font-mono text-[11px] text-muted-foreground sm:shrink-0 sm:text-right">
                  {a.resolvedAt ? (
                    <>
                      resolved <Ago iso={a.resolvedAt} />
                    </>
                  ) : a.firedAt ? (
                    <>
                      firing since <Ago iso={a.firedAt} />
                    </>
                  ) : (
                    <>
                      pending since <Ago iso={a.pendingSince} />
                    </>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </div>
  )
}

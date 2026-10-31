import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { SearchCheckIcon } from 'lucide-react'
import type { Container } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { ConfirmDialog, Empty, ErrorBanner, Loading, Notice, cn } from '#/components/kit'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import { useRole } from '#/routes/app/route'

type Action = 'start' | 'stop' | 'restart' | 'update'

export function ContainersTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const list = useQuery(q.containers(serverId))
  const [checkImages, setCheckImages] = useState(false)
  const images = useQuery({ ...q.imageCheck(serverId), enabled: checkImages })
  const [pending, setPending] = useState<{
    c: Container
    action: Action
  } | null>(null)

  const act = useMutation({
    mutationFn: ({ c, action }: { c: Container; action: Action }) =>
      call(
        api().POST('/servers/{serverId}/containers/{name}/{action}', {
          params: { path: { serverId, name: c.name, action } },
          body: { confirm: c.protected ? c.name : undefined },
        }),
      ),
    onSuccess: async () => {
      setPending(null)
      await qc.invalidateQueries({ queryKey: ['containers', serverId] })
      if (checkImages) await qc.invalidateQueries({ queryKey: ['image-check', serverId] })
    },
  })

  if (list.isLoading) return <Loading />
  if (list.error) return <ErrorBanner error={list.error} />
  const items = list.data?.items ?? []
  if (items.length === 0) return <Empty title="No containers">Docker is not installed, or nothing runs on it.</Empty>

  const imageFor = (name: string) => images.data?.items.find((i) => i.container === name)

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="font-mono text-xs text-muted-foreground">{items.length} containers</span>
        <Button size="sm" variant="outline" onClick={() => (checkImages ? void images.refetch() : setCheckImages(true))} busy={images.isFetching}>
          <SearchCheckIcon /> Check for newer images
        </Button>
      </div>
      <ErrorBanner error={images.error} />
      {act.isSuccess && act.data.output && (
        <Notice tone="ok">
          <pre className="font-mono text-[11px] break-all whitespace-pre-wrap">{act.data.output}</pre>
        </Notice>
      )}
      <Card>
        <ul className="divide-y divide-line/60">
          {items.map((c) => {
            const img = imageFor(c.name)
            const unhealthy = c.health === 'unhealthy' || c.state === 'restarting'
            return (
              <li
                key={c.id}
                className="grid gap-x-4 gap-y-2 px-4 py-3 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1.2fr)_minmax(0,0.9fr)_minmax(0,0.9fr)_auto] lg:items-center"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={cn('font-mono text-sm break-all', unhealthy ? 'text-fail' : c.state === 'running' ? 'text-ink' : 'text-muted-foreground')}>
                      {c.name}
                    </span>
                    {c.protected && <Badge variant="amber">protected</Badge>}
                  </div>
                  <div className="truncate font-mono text-[10px] text-faint">
                    {c.composeProject ? `${c.composeProject} / ${c.composeService}` : 'not compose'}
                  </div>
                </div>
                <div className="min-w-0">
                  <div className="truncate font-mono text-xs text-ink-2" title={c.image}>
                    {c.image}
                  </div>
                  {img &&
                    (img.updateAvailable ? (
                      <div className="font-mono text-[10px] text-warn">newer image available</div>
                    ) : img.error ? (
                      <div className="truncate font-mono text-[10px] text-faint">{img.error}</div>
                    ) : (
                      <div className="font-mono text-[10px] text-ok">image is current</div>
                    ))}
                </div>
                <div className="min-w-0 font-mono text-[11px]">
                  <span className={unhealthy ? 'text-fail' : c.state === 'running' ? 'text-ok' : 'text-muted-foreground'}>{c.health || c.state}</span>
                  <div className="truncate text-faint">{c.status}</div>
                </div>
                <div className="min-w-0 font-mono text-[11px] text-muted-foreground">
                  {c.restartCount > 0 && <span className="text-warn">{c.restartCount} restarts · </span>}
                  {c.restartPolicy || 'no restart'}
                  {c.startedAt && (
                    <div>
                      started <Ago iso={c.startedAt} />
                    </div>
                  )}
                  {c.ports.length > 0 && <div className="truncate text-[10px] text-faint">{c.ports.join(' ')}</div>}
                </div>
                {canAct && (
                  <div className="flex flex-wrap gap-1 lg:justify-end">
                    {(c.state === 'running' ? (['restart', 'stop'] as const) : (['start'] as const)).map((a) => (
                      <Button key={a} size="xs" variant="outline" onClick={() => setPending({ c, action: a })}>
                        {a}
                      </Button>
                    ))}
                    {c.composeProject && (
                      <Button size="xs" variant={img?.updateAvailable ? 'default' : 'outline'} onClick={() => setPending({ c, action: 'update' })}>
                        pull + recreate
                      </Button>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      </Card>
      <ConfirmDialog
        open={!!pending}
        title={pending?.action === 'update' ? `Update ${pending.c.name}` : `${pending?.action ?? ''} ${pending?.c.name ?? ''}`}
        danger={pending?.action !== 'start'}
        confirmWord={pending?.c.protected ? pending.c.name : undefined}
        body={
          pending?.action === 'update'
            ? 'Pulls the newest image for this compose service and recreates only this container. Other services stay as they are.'
            : pending?.c.protected
              ? 'This container is part of the public edge. Traffic through it stops while it restarts.'
              : 'The action is written to the activity log.'
        }
        actionLabel={pending?.action === 'update' ? 'Pull and recreate' : (pending?.action ?? '')}
        busy={act.isPending}
        error={act.error}
        onConfirm={() => pending && act.mutate(pending)}
        onClose={() => setPending(null)}
      />
    </div>
  )
}

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { Container } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { Button, ConfirmDialog, Empty, ErrorBanner, Loading, Notice, cx } from '#/components/ui'
import { useRole } from '#/routes/app/route'

type Action = 'start' | 'stop' | 'restart' | 'update'

export function ContainersTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const list = useQuery(q.containers(serverId))
  const [checkImages, setCheckImages] = useState(false)
  const images = useQuery({ ...q.imageCheck(serverId), enabled: checkImages })
  const [pending, setPending] = useState<{ c: Container; action: Action } | null>(null)

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
      <div className="flex items-center justify-between gap-3">
        <span className="font-mono text-xs text-muted">{items.length} containers</span>
        <Button size="sm" onClick={() => (checkImages ? void images.refetch() : setCheckImages(true))} busy={images.isFetching}>
          Check for newer images
        </Button>
      </div>
      <ErrorBanner error={images.error} />
      {act.isSuccess && act.data.output && (
        <Notice tone="ok">
          <pre className="whitespace-pre-wrap font-mono text-[11px]">{act.data.output}</pre>
        </Notice>
      )}
      <div className="panel overflow-x-auto">
        <table className="w-full min-w-[860px] text-left text-sm">
          <tbody>
            {items.map((c) => {
              const img = imageFor(c.name)
              const unhealthy = c.health === 'unhealthy' || c.state === 'restarting'
              return (
                <tr key={c.id} className="border-b border-line/60 last:border-0">
                  <td className="px-3 py-2.5">
                    <span className={cx('font-mono text-sm', unhealthy ? 'text-fail' : c.state === 'running' ? 'text-ink' : 'text-muted')}>{c.name}</span>
                    {c.protected && <span className="ml-2 font-mono text-[9px] uppercase text-amber">protected</span>}
                    <div className="font-mono text-[10px] text-faint">{c.composeProject ? `${c.composeProject} / ${c.composeService}` : 'not compose'}</div>
                  </td>
                  <td className="px-3 py-2.5">
                    <div className="truncate font-mono text-xs text-ink-2">{c.image}</div>
                    {img &&
                      (img.updateAvailable ? (
                        <div className="font-mono text-[10px] text-warn">newer image available</div>
                      ) : img.error ? (
                        <div className="font-mono text-[10px] text-faint">{img.error}</div>
                      ) : (
                        <div className="font-mono text-[10px] text-ok">image is current</div>
                      ))}
                  </td>
                  <td className="px-3 py-2.5 font-mono text-[11px]">
                    <span className={unhealthy ? 'text-fail' : c.state === 'running' ? 'text-ok' : 'text-muted'}>{c.health || c.state}</span>
                    <div className="text-faint">{c.status}</div>
                  </td>
                  <td className="px-3 py-2.5 font-mono text-[11px] text-muted">
                    {c.restartCount > 0 && <span className="text-warn">{c.restartCount} restarts · </span>}
                    {c.restartPolicy || 'no restart'}
                    {c.startedAt && <div>started <Ago iso={c.startedAt} /></div>}
                  </td>
                  <td className="px-3 py-2.5 font-mono text-[10px] text-faint">{c.ports.join(' ')}</td>
                  <td className="px-3 py-2.5 text-right">
                    {canAct && (
                      <div className="flex justify-end gap-1">
                        {(c.state === 'running' ? (['restart', 'stop'] as const) : (['start'] as const)).map((a) => (
                          <Button key={a} size="sm" variant="ghost" onClick={() => setPending({ c, action: a })}>
                            {a}
                          </Button>
                        ))}
                        {c.composeProject && (
                          <Button size="sm" variant={img?.updateAvailable ? 'primary' : 'ghost'} onClick={() => setPending({ c, action: 'update' })}>
                            pull + recreate
                          </Button>
                        )}
                      </div>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
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

import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Empty, ErrorBanner, Field, Panel, Toggle } from '#/components/kit'
import { Ago } from '#/components/time'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'

/** Whether warnings go out one by one or in one daily summary. */
export function WarningRouting({ canEdit }: { canEdit: boolean }) {
  const qc = useQueryClient()
  const r = useQuery(q.routing())
  const [daily, setDaily] = useState(false)
  const [hour, setHour] = useState(8)
  const [tz, setTz] = useState('UTC')
  useEffect(() => {
    if (!r.data) return
    setDaily(r.data.warnings === 'daily')
    setHour(r.data.hour)
    setTz(r.data.timezone)
  }, [r.data])
  const save = useMutation({
    mutationFn: () =>
      call(
        api().PUT('/alerts/routing', {
          body: {
            warnings: daily ? 'daily' : 'immediate',
            hour,
            timezone: tz.trim(),
          },
        }),
      ),
    onSuccess: (data) => qc.setQueryData(q.routing().queryKey, data),
  })
  return (
    <Panel title="Warning delivery">
      <form
        className="grid gap-4"
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate()
        }}
      >
        <Toggle
          checked={daily}
          onChange={setDaily}
          disabled={!canEdit}
          label="Send warnings in one daily summary"
          hint="Critical notices, host key changes, and new SSH keys are always sent at once. Held warnings are kept through a restart."
        />
        {daily && (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Hour (0 to 23)">
              <Input
                type="number"
                min={0}
                max={23}
                value={hour}
                disabled={!canEdit}
                onChange={(e) =>
                  setHour(
                    Math.min(23, Math.max(0, Number(e.target.value) || 0)),
                  )
                }
              />
            </Field>
            <Field label="Time zone" hint="For example Asia/Colombo or UTC.">
              <Input
                value={tz}
                disabled={!canEdit}
                onChange={(e) => setTz(e.target.value)}
              />
            </Field>
          </div>
        )}
        {canEdit && (
          <div>
            <Button type="submit" size="sm" busy={save.isPending}>
              Save
            </Button>
          </div>
        )}
        <ErrorBanner error={save.error ?? r.error} />
      </form>
    </Panel>
  )
}

const stateTone: Record<string, 'ok' | 'fail' | 'outline'> = {
  sent: 'ok',
  queued: 'outline',
  sending: 'outline',
  dead: 'fail',
}

/** Recent sends, and a retry for the ones that gave up. */
export function Deliveries() {
  const qc = useQueryClient()
  const [failed, setFailed] = useState(false)
  const r = useQuery(q.deliveries(failed))
  const retry = useMutation({
    mutationFn: (id: number) =>
      call(
        api().POST('/deliveries/{deliveryId}/retry', {
          params: { path: { deliveryId: id } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['deliveries'] }),
  })
  const items = r.data?.items ?? []
  return (
    <Panel
      title="Recent deliveries"
      actions={
        <Toggle checked={failed} onChange={setFailed} label="Failed only" />
      }
    >
      <ErrorBanner error={r.error ?? retry.error} className="mb-3" />
      {items.length === 0 ? (
        <Empty title={failed ? 'No failed deliveries' : 'Nothing sent yet'}>
          Sends to your channels show here, with their attempts and the last
          error.
        </Empty>
      ) : (
        <ul className="divide-y divide-line">
          {items.map((d) => (
            <li
              key={d.id}
              className="flex flex-col gap-1 py-2 sm:flex-row sm:items-center sm:gap-3"
            >
              <div className="min-w-0 flex-1">
                <div className="text-sm break-words text-ink">{d.title}</div>
                <div className="font-mono text-[10px] tracking-wider text-faint uppercase">
                  {d.channel} · {d.attempts} attempts ·{' '}
                  <Ago iso={d.createdAt} />
                  {d.lastError ? ` · ${d.lastError}` : ''}
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant={stateTone[d.state] ?? 'outline'}>
                  {d.state === 'dead' ? 'gave up' : d.state}
                </Badge>
                {d.state === 'dead' && (
                  <Button
                    size="sm"
                    variant="outline"
                    busy={retry.isPending && retry.variables === d.id}
                    onClick={() => retry.mutate(d.id)}
                  >
                    Retry
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}

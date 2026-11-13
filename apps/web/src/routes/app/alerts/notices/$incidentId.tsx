import { Link, createFileRoute } from '@tanstack/react-router'
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from '@tanstack/react-query'
import { useState } from 'react'
import { z } from 'zod'
import { api, call, orNotFound } from '#/lib/api'
import { q } from '#/lib/queries'
import {
  ErrorBanner,
  Field,
  KeyValue,
  Notice,
  PageHeader,
  Panel,
} from '#/components/kit'
import { Ago, When } from '#/components/time'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'

export const Route = createFileRoute('/app/alerts/notices/$incidentId')({
  params: {
    parse: (p) => ({
      incidentId: z.coerce.number().int().positive().parse(p.incidentId),
    }),
    stringify: (p) => ({ incidentId: String(p.incidentId) }),
  },
  loader: ({ context, params }) =>
    orNotFound(
      context.queryClient.ensureQueryData(q.incident(params.incidentId)),
    ),
  head: () => ({ meta: [{ title: 'Notice · easy-sys-admin' }] }),
  component: NoticePage,
})

const stateText: Record<string, string> = {
  open: 'Needs review',
  acknowledged: 'Reviewed',
  snoozed: 'Snoozed',
  accepted: 'Risk accepted',
  resolved: 'Resolved: a fresh check no longer saw it',
}

const eventText: Record<string, string> = {
  opened: 'Opened',
  reopened: 'Opened again',
  recurred: 'Came back after it was resolved',
  snooze_broken: 'Seen again, which ended the snooze',
  resolved: 'Resolved',
  acknowledged: 'Marked reviewed',
  snoozed: 'Snoozed',
  accepted: 'Risk accepted',
  returned: 'Back on the review list',
}

function NoticePage() {
  const { incidentId } = Route.useParams()
  const { data: n } = useSuspenseQuery(q.incident(incidentId))
  return (
    <div>
      <PageHeader
        kicker={
          <Link to="/app/alerts" className="hover:text-ink">
            Alerts
          </Link>
        }
        title={n.serverName}
        description={n.message}
      />
      <div className="grid gap-6 lg:grid-cols-[1fr_20rem]">
        <div className="min-w-0 space-y-6">
          <Panel title="Notice">
            <dl className="space-y-2">
              <KeyValue k="State" v={stateText[n.state] ?? n.state} wrap />
              <KeyValue k="Kind" v={n.kind} />
              {n.subject && <KeyValue k="Subject" v={n.subject} wrap />}
              <KeyValue k="First seen" v={<When iso={n.firstSeen} />} />
              <KeyValue k="Last seen" v={<When iso={n.lastSeen} />} />
              {n.resolvedAt && (
                <KeyValue k="Resolved" v={<When iso={n.resolvedAt} />} />
              )}
              {(n.recurrences ?? 0) > 0 && (
                <KeyValue k="Came back" v={`${n.recurrences} times`} />
              )}
              {n.snoozeUntil &&
                (n.state === 'snoozed' || n.state === 'accepted') && (
                  <KeyValue
                    k={
                      n.state === 'accepted'
                        ? 'Accepted until'
                        : 'Snoozed until'
                    }
                    v={<When iso={n.snoozeUntil} />}
                  />
                )}
            </dl>
            {n.snoozeReason && (
              <p className="mt-4 text-sm break-words text-ink-2">
                <span className="text-muted-foreground">Reason: </span>
                {n.snoozeReason}
              </p>
            )}
            {n.note && (
              <p className="mt-2 text-sm break-words text-ink-2">
                <span className="text-muted-foreground">Note: </span>
                {n.note}
              </p>
            )}
            <p className="mt-4 text-sm text-muted-foreground">{n.nextStep}</p>
          </Panel>
          <Panel title="History">
            <ol className="space-y-3">
              {(n.events ?? []).map((e, i) => (
                <li key={i} className="text-sm">
                  <div className="text-ink">
                    {eventText[e.kind] ?? e.kind}
                    <span className="text-muted-foreground"> · {e.actor}</span>
                  </div>
                  {e.detail && (
                    <div className="text-xs break-words text-muted-foreground">
                      {e.detail}
                    </div>
                  )}
                  <div className="font-mono text-[10px] text-faint">
                    <Ago iso={e.at} />
                  </div>
                </li>
              ))}
            </ol>
          </Panel>
        </div>
        <div className="space-y-6">
          <Link
            to="/app/servers/$serverId"
            params={{ serverId: n.serverId }}
            className="text-sm text-amber"
          >
            Open {n.serverName}
          </Link>
          {n.canReview && n.state !== 'resolved' ? (
            <Review id={n.id} kind={n.kind} />
          ) : !n.canReview ? (
            <Notice>
              The space that owns this server reviews this notice.
            </Notice>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function Review({ id, kind }: { id: number; kind: string }) {
  const qc = useQueryClient()
  const [note, setNote] = useState('')
  const [reason, setReason] = useState('')
  const [days, setDays] = useState(30)
  const done = () => qc.invalidateQueries({ queryKey: ['incidents'] })
  const path = { params: { path: { incidentId: id } } }
  const ack = useMutation({
    mutationFn: () =>
      call(
        api().POST('/incidents/{incidentId}/acknowledge', {
          ...path,
          body: { note: note.trim() },
        }),
      ),
    onSuccess: done,
  })
  const accept = useMutation({
    mutationFn: () =>
      call(
        api().POST('/incidents/{incidentId}/accept', {
          ...path,
          body: {
            until: new Date(
              Date.now() + days * 24 * 60 * 60 * 1000,
            ).toISOString(),
            reason: reason.trim(),
          },
        }),
      ),
    onSuccess: done,
  })
  const error = ack.error ?? accept.error
  return (
    <div className="space-y-6">
      <Panel title="Review">
        <form
          className="grid gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            ack.mutate()
          }}
        >
          <Field label="Note (optional)">
            <Input
              value={note}
              maxLength={500}
              onChange={(e) => setNote(e.target.value)}
            />
          </Field>
          <Button type="submit" busy={ack.isPending}>
            Mark reviewed
          </Button>
          <p className="text-xs text-muted-foreground">
            Reviewed is not fixed. It opens again if the same thing happens
            again.
          </p>
        </form>
      </Panel>
      {kind !== 'host_key' && (
        <Panel title="Accept the risk">
          <form
            className="grid gap-3"
            onSubmit={(e) => {
              e.preventDefault()
              if (reason.trim()) accept.mutate()
            }}
          >
            <Field label="Why">
              <Input
                value={reason}
                maxLength={200}
                onChange={(e) => setReason(e.target.value)}
              />
            </Field>
            <Field label="For how long">
              <select
                className="h-9 rounded-md border bg-bg-2 px-2 text-sm"
                value={days}
                onChange={(e) => setDays(Number(e.target.value))}
              >
                <option value={7}>7 days</option>
                <option value={30}>30 days</option>
                <option value={90}>90 days</option>
              </select>
            </Field>
            <Button
              type="submit"
              variant="outline"
              busy={accept.isPending}
              disabled={!reason.trim()}
            >
              Accept until then
            </Button>
            <p className="text-xs text-muted-foreground">
              The notice and its evidence stay. It comes back when the time is
              up or when it changes.
            </p>
          </form>
        </Panel>
      )}
      <ErrorBanner error={error} />
    </div>
  )
}

import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Schemas } from '@esa/api'
import { api, call } from '#/lib/api'
import { ErrorBanner, Field, KeyValue } from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import { Input } from '#/components/ui/input'

type Plan = Schemas['FixPlan']

/** Names for the ids in a plan. Unknown ids show as "#id". */
export type PlanNames = {
  group: (id: number) => string
  server: (id: number) => string
}

const PACKAGES_SHOWN = 30

/**
 * One plan with every step it would take, so a person approves what they
 * read, not a fingerprint.
 */
export function PlanCard({
  plan,
  names,
  canAct,
}: {
  plan: Plan
  names: PlanNames
  canAct: boolean
}) {
  const b = plan.body
  const servers = b.pinnedServerIds ?? b.serverIds ?? []
  return (
    <Card className="p-4">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 flex-1">
          <div className="text-sm text-ink">
            {kindLabel(b.kind)} ·{' '}
            {b.groupId != null
              ? `group ${names.group(b.groupId)}`
              : `${servers.length} ${servers.length === 1 ? 'server' : 'servers'}`}{' '}
            · {plan.status}
          </div>
          <dl className="mt-3 grid gap-1.5">
            <KeyValue
              k="Servers"
              wrap
              v={
                servers.length === 0
                  ? 'none recorded'
                  : servers.map(names.server).join(', ')
              }
            />
            {b.kind === 'update' && (
              <>
                <KeyValue k="Update mode" v={b.updateMode ?? 'security'} />
                <KeyValue
                  k="Held packages"
                  wrap
                  v={b.hold?.length ? b.hold.join(', ') : 'none'}
                />
                <KeyValue
                  k="Reboot if required"
                  v={b.rebootIfRequired ? 'yes' : 'no'}
                />
                {b.canaryServerIds?.length ? (
                  <KeyValue
                    k="Canaries"
                    wrap
                    v={b.canaryServerIds.map(names.server).join(', ')}
                  />
                ) : null}
                {b.observeSeconds ? (
                  <KeyValue k="Watch" v={`${b.observeSeconds} s`} />
                ) : null}
                {b.waitAfterCanaries ? (
                  <KeyValue k="After the canaries" v="waits for a person" />
                ) : null}
              </>
            )}
            {(b.kind === 'clean' || b.kind === 'measure') && (
              <>
                <KeyValue k="Build cache" v={b.buildCache ? 'yes' : 'no'} />
                <KeyValue k="VS Code copies" v={b.vscode ? 'yes' : 'no'} />
                <KeyValue
                  k="Journal"
                  v={
                    b.journal ? `yes, down to ${b.journalSize ?? '200M'}` : 'no'
                  }
                />
              </>
            )}
          </dl>
          {b.kind === 'update' && (
            <Packages names={b.packages ?? []} pending={b.pending?.length} />
          )}
          <div className="mt-3 font-mono text-[11px] break-all text-faint">
            {plan.fingerprint}
          </div>
          {plan.reason && (
            <p className="mt-1 text-xs text-muted-foreground">{plan.reason}</p>
          )}
          {plan.runId != null && (
            <Link
              to="/app/runs/$runId"
              params={{ runId: plan.runId }}
              className="mt-1 inline-block text-xs text-amber"
            >
              Run {plan.runId}
            </Link>
          )}
        </div>
        {canAct && plan.status === 'submitted' && <Decide id={plan.id} />}
        {canAct && plan.status === 'approved' && <Execute plan={plan} />}
        {canAct && plan.status === 'approved' && <Cancel id={plan.id} />}
      </div>
    </Card>
  )
}

function kindLabel(kind: Plan['body']['kind']) {
  return kind === 'update' ? 'Update' : kind === 'clean' ? 'Clean' : 'Measure'
}

function Packages({ names, pending }: { names: string[]; pending?: number }) {
  const [all, setAll] = useState(false)
  if (names.length === 0) {
    return (
      <p className="mt-3 text-xs text-muted-foreground">
        No pending packages were recorded for this plan.
      </p>
    )
  }
  const shown = all ? names : names.slice(0, PACKAGES_SHOWN)
  return (
    <div className="mt-3">
      <div className="font-mono text-[10px] tracking-[0.14em] text-muted-foreground uppercase">
        {pending && pending > names.length
          ? `Approves ${names.length} of ${pending} pending packages`
          : `Packages (${names.length})`}
      </div>
      <p className="mt-1 font-mono text-xs break-words text-ink-2">
        {shown.join(', ')}
      </p>
      {names.length > PACKAGES_SHOWN && (
        <button
          type="button"
          className="mt-1 text-xs text-amber"
          onClick={() => setAll(!all)}
        >
          {all ? 'Show fewer' : `Show all ${names.length}`}
        </button>
      )}
    </div>
  )
}

function Decide({ id }: { id: number }) {
  const qc = useQueryClient()
  const [rejecting, setRejecting] = useState(false)
  const [reason, setReason] = useState('')
  const done = () => qc.invalidateQueries({ queryKey: ['plans'] })
  const approve = useMutation({
    mutationFn: () =>
      call(
        api().POST('/plans/{planId}/approve', {
          params: { path: { planId: id } },
        }),
      ),
    onSuccess: done,
  })
  const reject = useMutation({
    mutationFn: () =>
      call(
        api().POST('/plans/{planId}/reject', {
          params: { path: { planId: id } },
          body: { reason: reason.trim() },
        }),
      ),
    onSuccess: done,
  })
  const error = approve.error ?? reject.error
  if (rejecting) {
    return (
      <form
        className="grid w-full gap-2 sm:w-72"
        onSubmit={(e) => {
          e.preventDefault()
          if (reason.trim()) reject.mutate()
        }}
      >
        <Field label="Why reject">
          <Input
            value={reason}
            maxLength={200}
            onChange={(e) => setReason(e.target.value)}
            autoFocus
          />
        </Field>
        <div className="flex gap-2">
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => setRejecting(false)}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            size="sm"
            busy={reject.isPending}
            disabled={!reason.trim()}
          >
            Reject
          </Button>
        </div>
        {error && <ErrorBanner error={error} />}
      </form>
    )
  }
  return (
    <div className="grid gap-2">
      <div className="flex gap-2">
        <Button size="sm" variant="outline" onClick={() => setRejecting(true)}>
          Reject
        </Button>
        <Button
          size="sm"
          busy={approve.isPending}
          onClick={() => approve.mutate()}
        >
          Approve
        </Button>
      </div>
      {error && <ErrorBanner error={error} />}
    </div>
  )
}

function Execute({ plan }: { plan: Plan }) {
  const qc = useQueryClient()
  const run = useMutation({
    mutationFn: () =>
      call(
        api().POST('/plans/{planId}/execute', {
          params: { path: { planId: plan.id } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['plans'] }),
  })
  const expires = plan.expiresAt ? new Date(plan.expiresAt) : null
  return (
    <div className="grid gap-2 sm:justify-items-end">
      <Button size="sm" busy={run.isPending} onClick={() => run.mutate()}>
        Run now
      </Button>
      {expires && (
        <p className="text-xs text-muted-foreground">
          Approval ends at {expires.toLocaleTimeString()}
        </p>
      )}
      {run.error && <ErrorBanner error={run.error} />}
    </div>
  )
}

function Cancel({ id }: { id: number }) {
  const qc = useQueryClient()
  const cancel = useMutation({
    mutationFn: () =>
      call(
        api().POST('/plans/{planId}/cancel', {
          params: { path: { planId: id } },
          body: { reason: 'cancelled before it ran' },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['plans'] }),
  })
  return (
    <div className="grid gap-2">
      <Button
        size="sm"
        variant="outline"
        busy={cancel.isPending}
        onClick={() => cancel.mutate()}
      >
        Cancel plan
      </Button>
      {cancel.error && <ErrorBanner error={cancel.error} />}
    </div>
  )
}
